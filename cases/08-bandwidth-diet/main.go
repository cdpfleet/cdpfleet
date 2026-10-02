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
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const pageURL = "https://www.bbc.com/news"
const pricePerGB = 3.0 // a typical residential proxy price, USD

var firstParty = []string{"bbc.com", "bbc.co.uk", "bbci.co.uk"} // the site's own domains and CDNs
var heavy = map[string]bool{"image": true, "media": true, "font": true}

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

type row struct {
	Variant    string `json:"variant"`
	Title      string `json:"title"`
	Requests   int    `json:"requests"`
	Blocked    int    `json:"blocked"`
	Kilobytes  int64  `json:"kilobytes"`
	DomReadyMs int64  `json:"dom_ready_ms"`
	Saved      string `json:"saved"`
	ProxyCost  string `json:"proxy_cost_per_100k_pages"`
}

// Load the page in a fresh context (empty cache) and count every byte on the wire.
func measure(browser playwright.Browser, label, block string) row {
	context, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	defer context.Close()
	if block != "" {
		context.Route("**/*", func(route playwright.Route) {
			r := route.Request()
			u, _ := url.Parse(r.URL())
			thirdParty := true
			for _, d := range firstParty {
				if strings.HasSuffix(u.Hostname(), d) {
					thirdParty = false
				}
			}
			if heavy[r.ResourceType()] || (block == "first-party" && thirdParty) {
				route.Abort()
			} else {
				route.Continue()
			}
		})
	}
	page, _ := context.NewPage()
	var mu sync.Mutex
	var bytesSeen int64
	requests, blocked := 0, 0
	count := func(req playwright.Request) {
		s, err := req.Sizes()
		if err != nil {
			return
		}
		mu.Lock()
		bytesSeen += int64(s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize)
		requests++
		mu.Unlock()
	}
	page.OnRequestFinished(count)
	page.OnRequestFailed(func(playwright.Request) { mu.Lock(); blocked++; mu.Unlock() })
	t := time.Now()
	// DOM ready, then a fixed 5 s for the rest to arrive: the same window for every variant
	// ('load' can wait forever on a blocked video).
	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(90000)}); err != nil {
		log.Fatal(err)
	}
	readyMs := time.Since(t).Milliseconds()
	page.WaitForTimeout(5000)
	title, _ := page.Title()
	page.RemoveListener("requestfinished", count) // stop counting before closing
	mu.Lock()
	defer mu.Unlock()
	return row{Variant: label, Title: title, Requests: requests, Blocked: blocked,
		Kilobytes: int64(math.Round(float64(bytesSeen) / 1024)), DomReadyMs: readyMs}
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
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

	rows := []row{
		measure(browser, "everything", ""),
		measure(browser, "no images, media or fonts", "heavy"),
		measure(browser, "…and first-party only", "first-party"),
	}
	full := float64(rows[0].Kilobytes)
	for i := range rows {
		kb := float64(rows[i].Kilobytes)
		rows[i].Saved = fmt.Sprintf("%.0f%%", (1-kb/full)*100)
		rows[i].ProxyCost = fmt.Sprintf("$%.0f", kb*100000/1024/1024*pricePerGB)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(rows)
	fmt.Print(buf.String())
}
