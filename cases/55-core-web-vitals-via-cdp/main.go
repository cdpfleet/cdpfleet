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
	"os"
	"strings"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

var pages = [][2]string{
	{"example.com", "https://example.com/"},
	{"books.toscrape.com", "https://books.toscrape.com/"},
	{"Wikipedia article", "https://en.wikipedia.org/wiki/Web_browser"},
	{"shifting demo", "https://shift.test/"}, // served by page.route below
}

// A made-up page whose banner arrives late and pushes the text down: a layout shift on purpose.
var shifting = `<!doctype html><meta charset="utf-8"><title>shift</title>
<body style="margin:0;font:18px sans-serif"><div id="slot"></div>
<h1>Article</h1>` + strings.Repeat("<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>", 30) + `
<script>setTimeout(() => { document.getElementById('slot').innerHTML = '<div style="height:300px;background:#fc0">Late banner</div>'; }, 600);</script>`

// Reads the vitals from the page's own performance timeline. `buffered: true` returns the
// entries recorded before the observer existed, so this runs after the load.
const vitalsJS = `new Promise((resolve) => {
  const nav = performance.getEntriesByType('navigation')[0];
  const fcp = performance.getEntriesByName('first-contentful-paint')[0];
  let lcp = null, cls = 0;
  new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e; }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  setTimeout(() => resolve(JSON.stringify({
    ttfb_ms: Math.round(nav.responseStart),
    connect_ms: Math.round(nav.connectEnd - nav.domainLookupStart),
    wait_ms: Math.round(nav.responseStart - nav.requestStart),
    fcp_ms: fcp ? Math.round(fcp.startTime) : null,
    lcp_ms: lcp ? Math.round(lcp.startTime) : null,
    lcp_element: lcp && lcp.element ? lcp.element.tagName.toLowerCase() : null,
    cls: Math.round(cls * 1000) / 1000,
    load_ms: Math.round(nav.loadEventEnd),
  })), 200);
})`

type row struct {
	Page       string  `json:"page"`
	TTFBMs     int     `json:"ttfb_ms"`
	ConnectMs  int     `json:"connect_ms"`
	WaitMs     int     `json:"wait_ms"`
	FCPMs      *int    `json:"fcp_ms"`
	LCPMs      *int    `json:"lcp_ms"`
	LCPElement *string `json:"lcp_element"`
	CLS        float64 `json:"cls"`
	LoadMs     int     `json:"load_ms"`
	ScriptMs   int64   `json:"script_ms"`
	Layouts    int64   `json:"layouts"`
	DOMNodes   int64   `json:"dom_nodes"`
	JSHeapMB   float64 `json:"js_heap_mb"`
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

func measure(browser playwright.Browser, name, url string) (row, error) {
	r := row{Page: name}
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{Viewport: &playwright.Size{Width: 1280, Height: 800}}) // cold cache
	if err != nil {
		return r, err
	}
	defer context.Close()
	context.Route("https://shift.test/**", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html; charset=utf-8"), Body: shifting})
	})
	page, err := context.NewPage()
	if err != nil {
		return r, err
	}
	// A CDP session over the normal Playwright connection: the browser's own counters.
	cdp, err := context.NewCDPSession(page)
	if err != nil {
		return r, err
	}
	if _, err := cdp.Send("Performance.enable", nil); err != nil {
		return r, err
	}
	if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)}); err != nil {
		return r, err
	}
	page.WaitForTimeout(1500) // let late content (and late shifts) land
	vitals, err := page.Evaluate(vitalsJS)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(vitals.(string)), &r); err != nil {
		return r, err
	}
	result, err := cdp.Send("Performance.getMetrics", nil)
	if err != nil {
		return r, err
	}
	metrics := map[string]float64{}
	for _, m := range result.(map[string]any)["metrics"].([]any) {
		mm := m.(map[string]any)
		metrics[mm["name"].(string)] = toFloat(mm["value"])
	}
	r.ScriptMs = int64(math.Round(metrics["ScriptDuration"] * 1000))
	r.Layouts = int64(metrics["LayoutCount"])
	r.DOMNodes = int64(metrics["Nodes"])
	r.JSHeapMB = math.Round(metrics["JSHeapUsedSize"]/1048576*10) / 10
	return r, nil
}

func toFloat(v any) float64 {
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

func run() ([]row, error) {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		return nil, err
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return nil, err
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return nil, err
	}
	defer browser.Close()
	rows := []row{}
	for _, p := range pages {
		r, err := measure(browser, p[0], p[1])
		if err != nil {
			return rows, err
		}
		rows = append(rows, r)
	}
	return rows, nil
}

func main() {
	rows, err := run()
	if err != nil {
		log.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"rows": rows})
	fmt.Print(buf.String())
}
