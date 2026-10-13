// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/playwright-community/playwright-go"
)

const site = "https://quotes.toscrape.com"
const next = "li.next a" // the scraper assumes every listing page has a "Next" link

var key = os.Getenv("CDPFLEET_API_KEY")

type Scrape struct {
	LastPage      int     `json:"last_page"`
	FailedAtS     float64 `json:"failed_at_s"`
	Error         string  `json:"error"`
	Quotes        int     `json:"quotes"`
	URLAtFailure  string  `json:"url_at_failure"`
	NextLinks     int     `json:"next_links"`
	PreviousLinks int     `json:"previous_links"`
}

type Recording struct {
	AppearedAfterS  int64    `json:"appeared_after_s"`
	DurationSeconds *float64 `json:"duration_seconds"`
	Width           *int     `json:"width"`
	Height          *int     `json:"height"`
	Bytes           *int64   `json:"bytes"`
	DownloadedBytes int      `json:"downloaded_bytes"`
	ContentType     string   `json:"content_type"`
	MP4             bool     `json:"mp4"`
	LinkExpiresInS  int      `json:"link_expires_in_s"`
	Deleted         bool     `json:"deleted"`
}

type Unrecorded struct {
	SessionID      string `json:"session_id"`
	RecordingFound bool   `json:"recording_found"`
}

type Output struct {
	Site                   string     `json:"site"`
	Scrape                 Scrape     `json:"scrape"`
	Recording              *Recording `json:"recording"`
	Unrecorded             Unrecorded `json:"unrecorded"`
	SessionID              string     `json:"session_id"`
	SessionSeconds         float64    `json:"session_seconds"`
	FailureWithinRecording *bool      `json:"failure_within_recording,omitempty"`
}

type apiRecording struct {
	ID              string   `json:"id"`
	SessionID       string   `json:"session_id"`
	DurationSeconds *float64 `json:"duration_seconds"`
	Width           *int     `json:"width"`
	Height          *int     `json:"height"`
	Bytes           *int64   `json:"bytes"`
}

func api(method, path string, into any) (int, error) {
	req, _ := http.NewRequest(method, "https://cdpfleet.com"+path, nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if into != nil {
		_ = json.NewDecoder(res.Body).Decode(into)
	}
	return res.StatusCode, nil
}

func recordings() []apiRecording {
	var body struct {
		Recordings []apiRecording `json:"recordings"`
	}
	_, _ = api("GET", "/v1/me/recordings", &body)
	return body.Recordings
}

func since(t time.Time) float64 { return math.Round(time.Since(t).Seconds()*10) / 10 }

type session struct {
	id      string
	started time.Time
	browser playwright.Browser
	page    playwright.Page
}

// Headful (the default) sessions are recorded from the browser's display.
func launch(pw *playwright.Playwright, record *bool) (*session, error) {
	body := map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": false}
	if record != nil {
		body["record"] = *record
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(payload))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		text, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch: %d %s", res.StatusCode, text)
	}
	var launched struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&launched); err != nil {
		return nil, err
	}
	started := time.Now()
	browser, err := pw.Chromium.Connect(launched.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return nil, err
	}
	// NoViewport: the page fills the whole display, so it fills the recording too.
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{NoViewport: playwright.Bool(true)})
	if err != nil {
		browser.Close()
		return nil, err
	}
	page, err := context.NewPage()
	if err != nil {
		browser.Close()
		return nil, err
	}
	return &session{launched.SessionID, started, browser, page}, nil
}

// Proxies drop a tunnel now and then: up to two retries.
func goTo(page playwright.Page, url string) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err = page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return err
}

func scrapeUntilItBreaks(s *session, out *Output) error {
	defer s.browser.Close() // ends the session; the node uploads the video afterwards
	if err := goTo(s.page, site+"/page/8/"); err != nil {
		return err
	}
	quotes := 0
	for pageNo := 8; ; pageNo++ {
		texts, err := s.page.Locator(".quote .text").AllTextContents()
		if err != nil {
			return err
		}
		quotes += len(texts)
		out.Scrape.LastPage = pageNo
		href, err := s.page.Locator(next).GetAttribute("href", playwright.LocatorGetAttributeOptions{Timeout: playwright.Float(5000)}) // 5 s to find the link
		if err != nil {
			out.Scrape.FailedAtS = since(s.started)
			kind := "error"
			if errors.Is(err, playwright.ErrTimeout) {
				kind = "timeout"
			}
			out.Scrape.Error = fmt.Sprintf("%s waiting for %s", kind, next)
			break
		}
		base, _ := url.Parse(site)
		ref, err := url.Parse(href)
		if err != nil {
			return err
		}
		if err := goTo(s.page, base.ResolveReference(ref).String()); err != nil { // page loads keep their own timeout and retries
			return err
		}
	}
	out.Scrape.Quotes = quotes
	// What the video shows at that moment: the last page — a "Previous" link and no "Next".
	out.Scrape.URLAtFailure = s.page.URL()
	out.Scrape.NextLinks, _ = s.page.Locator("li.next").Count()
	out.Scrape.PreviousLinks, _ = s.page.Locator("li.previous").Count()
	return nil
}

func run(out *Output) error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()

	// A session launched with record: false, for comparison: it must never get a recording.
	quiet, err := launch(pw, playwright.Bool(false))
	if err != nil {
		return err
	}
	err = goTo(quiet.page, site)
	quiet.browser.Close()
	if err != nil {
		return err
	}
	out.Unrecorded.SessionID = quiet.id

	// The scrape: pages 8, 9, 10 — follow "Next" on each. Page 10 is the last one and has none.
	s, err := launch(pw, nil)
	if err != nil {
		return err
	}
	out.SessionID = s.id
	if err := scrapeUntilItBreaks(s, out); err != nil {
		return err
	}
	out.SessionSeconds = since(s.started)

	// Find the recording, check it covers the failure, download it, delete it.
	t := time.Now()
	var rec *apiRecording
	for rec == nil && time.Since(t) < 10*time.Minute {
		for _, r := range recordings() {
			if r.SessionID == s.id {
				rec = &r
				break
			}
		}
		if rec == nil {
			time.Sleep(5 * time.Second)
		}
	}
	if rec != nil {
		var link struct {
			URL        string `json:"url"`
			ExpiresInS int    `json:"expires_in_s"`
		}
		if _, err := api("GET", "/v1/me/recordings/"+rec.ID+"/url", &link); err != nil {
			return err
		}
		res, err := http.Get(link.URL)
		if err != nil {
			return err
		}
		video, _ := io.ReadAll(res.Body)
		res.Body.Close()
		out.Recording = &Recording{
			AppearedAfterS:  int64(math.Round(time.Since(t).Seconds())),
			DurationSeconds: rec.DurationSeconds,
			Width:           rec.Width,
			Height:          rec.Height,
			Bytes:           rec.Bytes,
			DownloadedBytes: len(video),
			ContentType:     res.Header.Get("content-type"),
			MP4:             len(video) > 8 && string(video[4:8]) == "ftyp",
			LinkExpiresInS:  link.ExpiresInS,
		}
		within := rec.DurationSeconds != nil && out.Scrape.FailedAtS <= *rec.DurationSeconds+1
		out.FailureWithinRecording = &within
		status, _ := api("DELETE", "/v1/me/recordings/"+rec.ID, nil)
		out.Recording.Deleted = status == 200
	}

	// By now the scrape's video has been uploaded; give the unrecorded session another minute.
	time.Sleep(60 * time.Second)
	for _, r := range recordings() {
		if r.SessionID == quiet.id {
			out.Unrecorded.RecordingFound = true
		}
	}
	return nil
}

func main() {
	out := Output{Site: site, Recording: &Recording{}}
	if err := run(&out); err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
