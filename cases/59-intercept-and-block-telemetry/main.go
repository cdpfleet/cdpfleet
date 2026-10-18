// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_US
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

var sites = [][2]string{
	{"The Guardian", "https://www.theguardian.com/international"},
	{"NPR", "https://www.npr.org/"},
	{"Wikipedia", "https://en.wikipedia.org/wiki/Web_browser"},
}

// Analytics, measurement and real-user-monitoring hosts (subdomains included) — not ads.
var telemetryHosts = []string{"google-analytics.com", "analytics.google.com", "googletagmanager.com", "ophan.theguardian.com",
	"performance-events.guardianapis.com", "feast-events.guardianapis.com", "scorecardresearch.com", "permutive.app",
	"permutive.com", "go-mpulse.net", "akstat.io", "nr-data.net", "newrelic.com", "sentry.io", "datadoghq.com",
	"hotjar.com", "clarity.ms", "segment.io", "segment.com", "chartbeat.com", "chartbeat.net", "parsely.com",
	"analytics.yahoo.com", "omtrdc.net", "demdex.net", "bat.bing.com", "cloudflareinsights.com", "brandmetrics.com"}

// Telemetry: one of those hosts, or a beacon (navigator.sendBeacon, <a ping>).
func isTelemetry(req playwright.Request) bool {
	if req.ResourceType() == "ping" {
		return true
	}
	u, err := url.Parse(req.URL())
	if err != nil {
		return false
	}
	host := u.Hostname()
	for _, d := range telemetryHosts {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

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

type stats struct {
	requests, telemetry, onLeave, bytes, telemetryBytes, blocked int64
	domReady                                                     int64
	title                                                        string
	headings                                                     int
}

// One load in a fresh context: DOM ready + 6 s, then leave the page (about:blank) and give the
// page-hide beacons 1.5 s. Counts every request, the telemetry among them, and bytes on the wire.
func load(browser playwright.Browser, target string, block bool) stats {
	context, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	var st stats
	leaving := false
	if block {
		context.Route("**/*", func(route playwright.Route) {
			if isTelemetry(route.Request()) {
				mu.Lock()
				st.blocked++
				mu.Unlock()
				route.Abort()
			} else {
				route.Continue()
			}
		})
	}
	page, err := context.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	page.OnRequest(func(req playwright.Request) {
		mu.Lock()
		defer mu.Unlock()
		st.requests++
		if isTelemetry(req) {
			st.telemetry++
			if leaving {
				st.onLeave++
			}
		}
	})
	page.OnRequestFinished(func(req playwright.Request) {
		s, err := req.Sizes()
		if err != nil {
			return
		}
		n := int64(s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize)
		mu.Lock()
		defer mu.Unlock()
		st.bytes += n
		if isTelemetry(req) {
			st.telemetryBytes += n
		}
	})
	t := time.Now()
	if _, err := page.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(90000)}); err != nil {
		log.Fatal(err)
	}
	domReady := time.Since(t).Milliseconds()
	page.WaitForTimeout(6000)
	title, _ := page.Title()
	headings, _ := page.Locator("h1, h2, h3").Count() // the content is still there
	mu.Lock()
	leaving = true
	mu.Unlock()
	if _, err := page.Goto("about:blank"); err != nil {
		log.Fatal(err)
	}
	page.WaitForTimeout(1500)
	context.Close()
	mu.Lock()
	defer mu.Unlock()
	st.domReady, st.title, st.headings = domReady, title, headings
	return st
}

type row struct {
	Site                 string `json:"site"`
	TitleOK              bool   `json:"title_ok"`
	Requests             int64  `json:"requests"`
	TelemetryRequests    int64  `json:"telemetry_requests"`
	TelemetryOnLeave     int64  `json:"telemetry_on_leave"`
	TelemetryKB          int64  `json:"telemetry_kb"`
	TotalKB              int64  `json:"total_kb"`
	Blocked              int64  `json:"blocked"`
	Headings             int    `json:"headings"`
	HeadingsBlockedRun   int    `json:"headings_blocked_run"`
	DomReadyMs           int64  `json:"dom_ready_ms"`
	DomReadyMsBlockedRun int64  `json:"dom_ready_ms_blocked_run"`
}

func kb(n int64) int64 { return int64(math.Round(float64(n) / 1024)) }

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL_US"), "headless": true})
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

	rows := []row{}
	for _, site := range sites {
		off := load(browser, site[1], false)
		on := load(browser, site[1], true)
		rows = append(rows, row{
			Site: site[0], TitleOK: off.title != "" && off.title == on.title,
			Requests: off.requests, TelemetryRequests: off.telemetry, TelemetryOnLeave: off.onLeave,
			TelemetryKB: kb(off.telemetryBytes), TotalKB: kb(off.bytes), Blocked: on.blocked,
			Headings: off.headings, HeadingsBlockedRun: on.headings,
			DomReadyMs: off.domReady, DomReadyMsBlockedRun: on.domReady,
		})
	}
	out, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Println(string(out))
}
