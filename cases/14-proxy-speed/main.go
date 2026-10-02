// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const pageURL = "https://en.wikipedia.org/wiki/Web_browser"
const loads = 3 // per proxy, each in a fresh context (cold cache, new connections)

// Navigation Timing + Largest Contentful Paint, read in the page after load.
const timings = `() => new Promise((done) => {
  const nav = performance.getEntriesByType('navigation')[0];
  new PerformanceObserver((list) => {
    const lcp = list.getEntries().at(-1);
    done({
      connect: nav.connectEnd - nav.connectStart,
      ttfb: nav.responseStart - nav.requestStart,
      domReady: nav.domContentLoadedEventEnd,
      load: nav.loadEventEnd,
      lcp: lcp.startTime,
    });
  }).observe({ type: 'largest-contentful-paint', buffered: true });
})`

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

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// Evaluate returns whole numbers as int and others as float64.
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func measure(pw *playwright.Playwright, label, proxy string) map[string]any {
	fail := func(err error) map[string]any {
		return map[string]any{"proxy": label, "error": strings.SplitN(err.Error(), "\n", 2)[0]}
	}
	session, err := launch("chromium", map[string]any{"proxy": proxy, "headless": true})
	if err != nil {
		return fail(err)
	}
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fail(err)
	}
	defer browser.Close()
	runs := []map[string]any{}
	failures := 0
	for len(runs) < loads {
		context, err := browser.NewContext()
		if err != nil {
			return fail(err)
		}
		page, _ := context.NewPage()
		_, err = page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)})
		var v any
		if err == nil {
			v, err = page.Evaluate(timings)
		}
		context.Close()
		if err != nil {
			if failures++; failures > 1 { // one failed load is the proxy's noise; two is a problem
				return fail(err)
			}
			continue
		}
		runs = append(runs, v.(map[string]any))
	}
	median := func(k string) int64 {
		xs := []float64{}
		for _, r := range runs {
			xs = append(xs, num(r[k]))
		}
		sort.Float64s(xs)
		return int64(math.Round(xs[len(xs)/2]))
	}
	return map[string]any{"proxy": label, "loads": len(runs), "connect_ms": median("connect"), "ttfb_ms": median("ttfb"),
		"dom_ready_ms": median("domReady"), "lcp_ms": median("lcp"), "load_ms": median("load")}
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	proxies := [][2]string{
		{"residential (any country)", os.Getenv("PROXY_URL")},
		{"residential (Germany)", os.Getenv("PROXY_URL_DE")},
		{"datacenter SOCKS5", strings.Split(os.Getenv("SOCKS_PROXIES"), ",")[0]},
	}
	rows := make([]map[string]any, len(proxies))
	var wg sync.WaitGroup
	for i, p := range proxies {
		wg.Add(1)
		go func(i int, label, proxy string) { defer wg.Done(); rows[i] = measure(pw, label, proxy) }(i, p[0], p[1])
	}
	wg.Wait()
	out, _ := json.MarshalIndent(map[string]any{"page": pageURL, "statistic": fmt.Sprintf("median of %d cold loads", loads), "results": rows}, "", "  ")
	fmt.Println(string(out))
}
