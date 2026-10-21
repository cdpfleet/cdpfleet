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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var rates = []int{1, 4, 6}

const url = "https://en.wikipedia.org/wiki/Web_browser"

// A fixed amount of JavaScript work, timed inside the page: what a heavy client-side app does.
const workJS = `(() => {
  const t = performance.now();
  let h = 0;
  for (let i = 0; i < 3e6; i++) h = (h * 31 + i) % 1000003;
  const a = Array.from({ length: 200000 }, (_, i) => (i * 7919) % 100003).sort((x, y) => x - y);
  return JSON.stringify({ ms: Math.round(performance.now() - t), check: h + a[1000] });
})()`

const navJS = `JSON.stringify((() => {
  const n = performance.getEntriesByType('navigation')[0];
  return { response_ms: Math.round(n.responseEnd), dcl_ms: Math.round(n.domContentLoadedEventEnd), load_ms: Math.round(n.loadEventEnd) };
})())`

type work struct {
	Ms    int64 `json:"ms"`
	Check int64 `json:"check"`
}

type nav struct {
	ResponseMs int64 `json:"response_ms"`
	DCLMs      int64 `json:"dcl_ms"`
	LoadMs     int64 `json:"load_ms"`
}

type row struct {
	Rate         int   `json:"rate"`
	WorkMs       int64 `json:"work_ms"`
	ResponseMs   int64 `json:"response_ms"`
	DCLMs        int64 `json:"dcl_ms"`
	LoadMs       int64 `json:"load_ms"`
	ParseMs      int64 `json:"parse_ms"`
	MainThreadMs int64 `json:"main_thread_ms"`
	Check        int64 `json:"check"`
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

// evalJSON runs a script that returns JSON.stringify(...) and decodes it into v.
func evalJSON(page playwright.Page, script string, v any) error {
	s, err := page.Evaluate(script)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(s.(string)), v)
}

func taskDuration(cdp playwright.CDPSession) (float64, error) {
	result, err := cdp.Send("Performance.getMetrics", nil)
	if err != nil {
		return 0, err
	}
	for _, m := range result.(map[string]any)["metrics"].([]any) {
		mm := m.(map[string]any)
		if mm["name"] == "TaskDuration" {
			switch v := mm["value"].(type) {
			case float64:
				return v, nil
			case int:
				return float64(v), nil
			}
		}
	}
	return 0, nil
}

func measure(browser playwright.Browser, rate int) (row, error) {
	r := row{Rate: rate}
	context, err := browser.NewContext()
	if err != nil {
		return r, err
	}
	defer context.Close()
	page, err := context.NewPage()
	if err != nil {
		return r, err
	}
	// CPU throttling is a DevTools feature: reach it with a CDP session over the Playwright connection.
	cdp, err := context.NewCDPSession(page)
	if err != nil {
		return r, err
	}
	if _, err := cdp.Send("Performance.enable", nil); err != nil {
		return r, err
	}
	if _, err := cdp.Send("Emulation.setCPUThrottlingRate", map[string]any{"rate": rate}); err != nil {
		return r, err
	}
	// A real page load: the network part isn't throttled, the main-thread work is.
	if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(90000)}); err != nil {
		return r, err
	}
	var n nav
	if err := evalJSON(page, navJS, &n); err != nil {
		return r, err
	}
	td, err := taskDuration(cdp)
	if err != nil {
		return r, err
	}
	var w work
	if err := evalJSON(page, workJS, &w); err != nil {
		return r, err
	}
	// Load it once more and keep the shorter parse time: parsing waits for blocking scripts, and one
	// slow download through the proxy can inflate a single load at any rate.
	if _, err := page.Reload(playwright.PageReloadOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(90000)}); err != nil {
		return r, err
	}
	var again nav
	if err := evalJSON(page, navJS, &again); err != nil {
		return r, err
	}
	r.WorkMs, r.Check = w.Ms, w.Check
	r.ResponseMs, r.DCLMs, r.LoadMs = n.ResponseMs, n.DCLMs, n.LoadMs
	r.ParseMs = min(n.DCLMs-n.ResponseMs, again.DCLMs-again.ResponseMs) // HTML fully received → DOMContentLoaded: parsing and scripts
	r.MainThreadMs = int64(math.Round(td * 1000))                       // busy time while loading, from CDP
	return r, nil
}

func run() (map[string]any, error) {
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
	for _, rate := range rates {
		r, err := measure(browser, rate)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}

	// Is it per page? A throttled tab next to an unthrottled one in the same browser.
	context, err := browser.NewContext()
	if err != nil {
		return nil, err
	}
	defer context.Close()
	slow, _ := context.NewPage()
	fast, _ := context.NewPage()
	cdp, err := context.NewCDPSession(slow)
	if err != nil {
		return nil, err
	}
	if _, err := cdp.Send("Emulation.setCPUThrottlingRate", map[string]any{"rate": 6}); err != nil {
		return nil, err
	}
	var s, f work
	if err := evalJSON(slow, workJS, &s); err != nil {
		return nil, err
	}
	if err := evalJSON(fast, workJS, &f); err != nil {
		return nil, err
	}
	return map[string]any{"rows": rows, "side_by_side": map[string]int64{"throttled_tab_ms": s.Ms, "other_tab_ms": f.Ms}}, nil
}

func main() {
	out, err := run()
	if err != nil {
		log.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
