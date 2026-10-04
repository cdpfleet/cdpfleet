// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const start = "https://quotes.toscrape.com/scroll" // loads 10 quotes per screen, 100 in all
const quotesJS = "els => els.map(e => ({ text: e.querySelector('.text').textContent, author: e.querySelector('.author').textContent }))"

func launch(name string, options map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(options)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+name+"/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch %s: %s %s", name, res.Status, msg)
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

type scrollRow struct {
	Method         string  `json:"method"`
	Quotes         int     `json:"quotes"`
	Authors        int     `json:"authors"`
	Scrolls        int     `json:"scrolls"`
	Requests       int     `json:"requests"`
	LoadSeconds    float64 `json:"load_seconds"`
	CollectSeconds float64 `json:"collect_seconds"`
}

type apiRow struct {
	Method         string  `json:"method"`
	Quotes         int     `json:"quotes"`
	Authors        int     `json:"authors"`
	APIPages       int     `json:"api_pages"`
	Endpoint       string  `json:"endpoint"`
	Requests       int     `json:"requests"`
	LoadSeconds    float64 `json:"load_seconds"`
	CollectSeconds float64 `json:"collect_seconds"`
}

func seconds(d time.Duration) float64 { return math.Round(d.Seconds()*1000) / 1000 }

// Counts every request the tab makes.
func countRequests(page playwright.Page) func() int {
	var mu sync.Mutex
	n := 0
	page.OnRequest(func(playwright.Request) { mu.Lock(); n++; mu.Unlock() })
	return func() int { mu.Lock(); defer mu.Unlock(); return n }
}

// Way 1: behave like a user — scroll to the bottom until nothing more appears, then read the DOM.
func byScrolling(page playwright.Page) scrollRow {
	t := time.Now()
	requests := countRequests(page)
	if _, err := page.Goto(start, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}
	if err := page.Locator(".quote").First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}
	loaded := time.Now()
	count, scrolls, stale := 0, 0, 0
	for stale < 3 {
		page.Evaluate("window.scrollTo(0, document.body.scrollHeight)")
		scrolls++
		page.WaitForTimeout(600)
		now, _ := page.Locator(".quote").Count()
		if now > count {
			stale = 0
		} else {
			stale++
		}
		count = now
	}
	v, err := page.EvalOnSelectorAll(".quote", quotesJS)
	if err != nil {
		log.Fatal(err)
	}
	done := time.Now()
	quotes := v.([]any)
	authors := map[string]bool{}
	for _, q := range quotes {
		authors[q.(map[string]any)["author"].(string)] = true
	}
	return scrollRow{Method: "scroll the page", Quotes: len(quotes), Authors: len(authors), Scrolls: scrolls, Requests: requests(),
		LoadSeconds: seconds(loaded.Sub(t)), CollectSeconds: seconds(done.Sub(loaded))}
}

// Way 2: catch the JSON request the page makes for its first screen, then call that endpoint
// yourself from inside the browser (same proxy, cookies and TLS fingerprint as the page),
// several pages at a time. No scrolling, no guessing when loading has finished.
func byAPI(page playwright.Page) apiRow {
	t := time.Now()
	requests := countRequests(page)
	// playwright-go matches responses by URL only; the page's one /api/ request is its XHR.
	response, err := page.ExpectResponse(func(u string) bool { return strings.Contains(u, "/api/") }, func() error {
		_, err := page.Goto(start, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		return err
	}, playwright.PageExpectResponseOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	loaded := time.Now()
	endpoint, _ := url.Parse(response.URL())
	var first map[string]any
	if err := response.JSON(&first); err != nil {
		log.Fatal(err)
	}
	quotes := append([]any{}, first["quotes"].([]any)...) // page 1 came for free
	const wave = 4 // one round trip through the proxy per wave, not per page
	next := 2
	for more := true; more; next += wave {
		urls := make([]string, wave)
		for i := range urls {
			u := *endpoint
			q := u.Query()
			q.Set("page", strconv.Itoa(next+i))
			u.RawQuery = q.Encode()
			urls[i] = u.String()
		}
		v, err := page.Evaluate("us => Promise.all(us.map(u => fetch(u).then(r => r.json())))", urls)
		if err != nil {
			log.Fatal(err)
		}
		more = true
		for _, p := range v.([]any) {
			page := p.(map[string]any)
			quotes = append(quotes, page["quotes"].([]any)...)
			more = more && page["has_next"].(bool)
		}
	}
	done := time.Now()
	authors := map[string]bool{}
	for _, q := range quotes {
		authors[q.(map[string]any)["author"].(map[string]any)["name"].(string)] = true
	}
	return apiRow{Method: "call its JSON API", Quotes: len(quotes), Authors: len(authors), APIPages: next - 1,
		Endpoint: endpoint.Path + "?page=N", Requests: requests(),
		LoadSeconds: seconds(loaded.Sub(t)), CollectSeconds: seconds(done.Sub(loaded))}
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		log.Fatal(err)
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()

	var out []any
	page, _ := browser.NewPage() // a fresh tab per method, so request counts don't mix
	out = append(out, byScrolling(page))
	page.Close()
	page, _ = browser.NewPage()
	out = append(out, byAPI(page))
	page.Close()

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
