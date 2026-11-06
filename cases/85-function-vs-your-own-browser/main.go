// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const api = "https://cdpfleet.com"
const starter = "https://starter.cdpfleet.com"

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL") // the same proxy for the Function calls and the sessions
var headers = map[string]string{"x-api-key": key}
var pages = [][2]string{
	{"example.com", "https://example.com/"},
	{"books.toscrape.com", "https://books.toscrape.com/"},
	{"wikipedia", "https://en.wikipedia.org/wiki/Web_scraping"},
}
var tasks = []string{"screenshot", "pdf", "markdown"}
var since = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

type record struct {
	Engine        string
	Weight        int
	ThreadSeconds float64
}

// The record of a session or a Function call in the account's history (a call's x-call-id is its
// session id there). Billed thread-seconds = (ended_at - started_at) × weight.
func billed(id string) *record {
	for i := 0; i < 40; i++ {
		req, _ := http.NewRequest("GET", api+"/v1/me/sessions?limit=200&from="+url.QueryEscape(since), nil)
		req.Header.Set("x-api-key", key)
		res := must(http.DefaultClient.Do(req))
		var r struct {
			Sessions []struct {
				SessionID string     `json:"session_id"`
				Engine    string     `json:"engine"`
				Weight    int        `json:"weight"`
				StartedAt time.Time  `json:"started_at"`
				EndedAt   *time.Time `json:"ended_at"`
			} `json:"sessions"`
		}
		json.NewDecoder(res.Body).Decode(&r)
		res.Body.Close()
		for _, s := range r.Sessions {
			if s.SessionID == id && s.EndedAt != nil {
				return &record{s.Engine, s.Weight, round1(s.EndedAt.Sub(s.StartedAt).Seconds() * float64(s.Weight))}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

// One capture's numbers; the task-specific fields are left out where they don't apply.
type result struct {
	Status        int      `json:"status,omitempty"`
	Attempts      int      `json:"attempts,omitempty"`
	Ms            int64    `json:"ms"`
	LaunchMs      *int64   `json:"launch_ms,omitempty"`
	Bytes         int      `json:"bytes"`
	ContentType   *string  `json:"content_type,omitempty"`
	Partial       any      `json:"partial,omitempty"` // functions only: the x-cdpfleet-partial header or null
	Engine        *string  `json:"engine"`
	Weight        *int     `json:"weight"`
	ThreadSeconds *float64 `json:"thread_seconds"`
	Width         *int     `json:"width,omitempty"`
	Height        *int     `json:"height,omitempty"`
	TextPDF       *bool    `json:"text_pdf,omitempty"`
	Pages         *int     `json:"pages,omitempty"`
	ImageHeight   *int     `json:"image_height,omitempty"`
	HTMLBytes     *int     `json:"html_bytes,omitempty"`
	TextBytes     *int     `json:"text_bytes,omitempty"`
	FirstLine     *string  `json:"first_line,omitempty"`
	HasSource     *bool    `json:"has_source_line,omitempty"`
}

func ptr[T any](v T) *T { return &v }

var pageObj = regexp.MustCompile(`/Type\s*/Page([^s\w]|$)`)
var heightRe = regexp.MustCompile(`/Height (\d+)`)
var fontRe = regexp.MustCompile(`/Font\b`)

// What a file is: PNG width × height; for a PDF, whether it has fonts (a text PDF), its page objects
// (0 when they sit in compressed object streams) and the tallest image in it.
func describe(task string, buf []byte, r *result) {
	switch task {
	case "screenshot":
		r.Width = ptr(int(binary.BigEndian.Uint32(buf[16:20])))
		r.Height = ptr(int(binary.BigEndian.Uint32(buf[20:24])))
	case "pdf":
		h := 0
		for _, m := range heightRe.FindAllSubmatch(buf, -1) {
			var v int
			fmt.Sscan(string(m[1]), &v)
			h = max(h, v)
		}
		r.TextPDF = ptr(fontRe.Match(buf))
		r.Pages = ptr(len(pageObj.FindAll(buf, -1)))
		r.ImageHeight = ptr(h)
	}
}

// (a) The Function: one POST, the file in the response. One retry if the call fails.
func viaFunction(task, u string) result {
	body := map[string]any{"url": u, "proxy": proxy}
	if task == "screenshot" {
		body["full_page"] = true
	}
	if task == "pdf" {
		body["format"] = "A4"
	}
	payload := must(json.Marshal(body))
	t := time.Now()
	var calls []string
	var res *http.Response
	var buf []byte
	for attempt := 1; attempt <= 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "POST", api+"/v1/functions/"+task, bytes.NewReader(payload))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res = must(http.DefaultClient.Do(req))
		buf = must(io.ReadAll(res.Body))
		res.Body.Close()
		cancel()
		if id := res.Header.Get("x-call-id"); id != "" {
			calls = append(calls, id)
		}
		if res.StatusCode == 200 {
			break
		}
	}
	r := result{Status: res.StatusCode, Attempts: len(calls), Ms: time.Since(t).Milliseconds(), Bytes: len(buf), ContentType: ptr(res.Header.Get("content-type"))}
	r.Partial = json.RawMessage("null")
	if p := res.Header.Get("x-cdpfleet-partial"); p != "" {
		r.Partial = p
	}
	total, all := 0.0, true
	for _, id := range calls {
		rec := billed(id)
		if rec == nil {
			all = false
			continue
		}
		total += rec.ThreadSeconds
		r.Engine, r.Weight = ptr(rec.Engine), ptr(rec.Weight)
	}
	if all && len(calls) > 0 {
		r.ThreadSeconds = ptr(round1(total))
	}
	if res.StatusCode == 200 {
		describe(task, buf, &r)
		if task == "markdown" {
			md := string(buf)
			first := strings.SplitN(md, "\n", 2)[0]
			if len(first) > 80 {
				first = first[:80]
			}
			r.FirstLine = &first
			r.HasSource = ptr(strings.Contains(md, "\nSource: "))
		}
	}
	return r
}

// (b) Your own session: launch over HTTP, connect with the key header.
func launch(pw *playwright.Playwright) (string, playwright.Browser) {
	payload := must(json.Marshal(map[string]any{"proxy": proxy, "headless": true}))
	req, _ := http.NewRequest("POST", starter+"/chromium/session", bytes.NewReader(payload))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		log.Fatalf("launch %d %s", res.StatusCode, b)
	}
	var s struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
	}
	json.NewDecoder(res.Body).Decode(&s)
	return s.SessionID, must(pw.Chromium.Connect(s.WsURL, playwright.BrowserTypeConnectOptions{Headers: headers}))
}

func open(browser playwright.Browser, u string) playwright.Page {
	page := must(browser.NewPage(playwright.BrowserNewPageOptions{Viewport: &playwright.Size{Width: 1280, Height: 720}}))
	// A proxy drops the odd connection: one retry.
	if _, err := page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
		must(page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}))
	}
	return page
}

// Markdown has no Playwright call: you'd add a converter (html-to-markdown, …). Measured here: the
// HTML you would convert and the page's innerText.
func capture(page playwright.Page, task string) result {
	var r result
	switch task {
	case "screenshot":
		buf := must(page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true)}))
		r.Bytes = len(buf)
		describe(task, buf, &r)
	case "pdf":
		buf := must(page.PDF(playwright.PagePdfOptions{Format: playwright.String("A4")}))
		r.Bytes = len(buf)
		describe(task, buf, &r)
	default:
		html := len(must(page.Content()))
		text := len(must(page.Locator("body").InnerText()))
		r.Bytes, r.HTMLBytes, r.TextBytes = html, &html, &text
	}
	return r
}

func viaSession(pw *playwright.Playwright, task, u string) result {
	t := time.Now()
	id, browser := launch(pw)
	launchMs := time.Since(t).Milliseconds()
	r := capture(open(browser, u), task)
	browser.Close() // closing the connection ends the session
	r.Ms, r.LaunchMs = time.Since(t).Milliseconds(), &launchMs
	if rec := billed(id); rec != nil {
		r.Engine, r.Weight, r.ThreadSeconds = ptr(rec.Engine), ptr(rec.Weight), ptr(rec.ThreadSeconds)
	}
	return r
}

type run struct {
	Page     string `json:"page"`
	Task     string `json:"task"`
	Function result `json:"function"`
	Session  result `json:"session"`
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()

	var runs []run
	for _, p := range pages {
		for _, task := range tasks {
			runs = append(runs, run{p[0], task, viaFunction(task, p[1]), viaSession(pw, task, p[1])})
		}
	}

	// (c) One session for all nine captures: launch once, three pages, three outputs each.
	t := time.Now()
	id, browser := launch(pw)
	captures := 0
	for _, p := range pages {
		page := open(browser, p[1])
		for _, task := range tasks {
			capture(page, task)
			captures++
		}
		page.Close()
	}
	browser.Close()
	oneMs := time.Since(t).Milliseconds()
	one := billed(id)
	oneSession := map[string]any{"captures": captures, "ms": oneMs, "thread_seconds": nil, "weight": nil}
	if one != nil {
		oneSession["thread_seconds"], oneSession["weight"] = one.ThreadSeconds, one.Weight
	}

	out := struct {
		Language   string         `json:"language"`
		Proxy      string         `json:"proxy"`
		Runs       []run          `json:"runs"`
		OneSession map[string]any `json:"one_session"`
		MeasuredAt string         `json:"measured_at"`
	}{"go", "the same PROXY_URL for every Function call and every session", runs, oneSession, time.Now().UTC().Format("2006-01-02")}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
