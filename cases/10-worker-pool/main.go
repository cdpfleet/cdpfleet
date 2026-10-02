// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const pages = 24

type stats struct {
	mu                                                                     sync.Mutex
	launches, launchMs, connectMs, sessionMs, pageRetries, connectFailures int64
	retries                                                                map[string]int
	titles                                                                 []string
}

// Launch with the retries the API asks for: 429 (thread limit, launch rate) and 503
// (fleet momentarily busy) carry Retry-After.
func launch(s *stats) (string, error) {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
	for attempt := 1; ; attempt++ {
		t := time.Now()
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			atomic.AddInt64(&s.launches, 1)
			atomic.AddInt64(&s.launchMs, time.Since(t).Milliseconds())
			return out["wsUrl"].(string), nil
		}
		errName, _ := out["error"].(string)
		retryable := res.StatusCode == 503 || (res.StatusCode == 429 && !strings.Contains(errName, "quota"))
		if !retryable || attempt == 10 {
			return "", fmt.Errorf("launch: %d %s", res.StatusCode, errName)
		}
		s.mu.Lock()
		s.retries[errName]++
		s.mu.Unlock()
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 2
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// Residential proxies drop a tunnel now and then (ERR_TUNNEL_CONNECTION_FAILED): retry.
func scrape(page playwright.Page, s *stats) string {
	for attempt := 1; ; attempt++ {
		_, err := page.Goto("https://en.wikipedia.org/wiki/Special:Random", playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
		if err == nil {
			title, _ := page.Title()
			return title
		}
		atomic.AddInt64(&s.pageRetries, 1)
		if attempt == 3 {
			return "(failed: " + strings.SplitN(err.Error(), "\n", 2)[0] + ")"
		}
	}
}

// Runs pages pages on workers parallel workers; each worker either opens one session
// and reuses it, or opens a new session for every page.
func run(pw *playwright.Playwright, workers int, reuse bool) map[string]any {
	s := &stats{retries: map[string]int{}}
	var next int64 = -1
	// If the connect fails (rare: the server holding the browser didn't answer), don't
	// reconnect to the same wsUrl: launch a fresh session. Such sessions aren't billed.
	open := func() (playwright.Browser, time.Time) {
		for attempt := 1; ; attempt++ {
			ws, err := launch(s)
			if err != nil {
				log.Fatal(err)
			}
			t := time.Now()
			browser, err := pw.Chromium.Connect(ws, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
			if err == nil {
				atomic.AddInt64(&s.connectMs, time.Since(t).Milliseconds())
				return browser, t
			}
			atomic.AddInt64(&s.connectFailures, 1)
			if attempt == 3 {
				log.Fatal(err)
			}
		}
	}
	closeSession := func(b playwright.Browser, started time.Time) {
		b.Close()
		atomic.AddInt64(&s.sessionMs, time.Since(started).Milliseconds())
	}
	add := func(title string) { s.mu.Lock(); s.titles = append(s.titles, title); s.mu.Unlock() }

	worker := func() {
		if reuse {
			browser, started := open()
			defer closeSession(browser, started)
			page, _ := browser.NewPage()
			for atomic.AddInt64(&next, 1) < pages {
				add(scrape(page, s))
			}
			return
		}
		for atomic.AddInt64(&next, 1) < pages {
			browser, started := open()
			page, _ := browser.NewPage()
			add(scrape(page, s))
			closeSession(browser, started)
		}
	}

	t := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); worker() }()
	}
	wg.Wait()
	strategy := "new session per page"
	if reuse {
		strategy = "reuse one session per worker"
	}
	return map[string]any{
		"strategy":              strategy,
		"pages":                 len(s.titles),
		"wall_seconds":          math.Round(time.Since(t).Seconds()*10) / 10,
		"launches":              s.launches,
		"avg_launch_ms":         s.launchMs / s.launches,
		"avg_connect_ms":        s.connectMs / s.launches,
		"launch_retries":        s.retries,
		"connect_failures":      s.connectFailures,
		"page_retries":          s.pageRetries,
		"billed_thread_seconds": int64(math.Round(float64(s.sessionMs) / 1000)),
		"sample_titles":         s.titles[:min(3, len(s.titles))],
	}
}

func main() {
	// Size the pool from the plan: never more workers than threads.
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me", nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	var me struct {
		Subscription struct{ Threads int } `json:"subscription"`
	}
	json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	workers := min(me.Subscription.Threads, 6)

	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	out, _ := json.MarshalIndent(map[string]any{
		"plan_threads": me.Subscription.Threads,
		"workers":      workers,
		"results":      []map[string]any{run(pw, workers, false), run(pw, workers, true)},
	}, "", "  ")
	fmt.Println(string(out))
}
