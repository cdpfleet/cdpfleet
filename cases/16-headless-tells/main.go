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
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL")

// Four ways to run a browser; the same twelve signals read from inside the page.
type target struct {
	label, engine, family, prefix string
	body                          map[string]any
}

var targets = []target{
	{"Chromium, headless", "chromium", "chromium", "", map[string]any{"headless": "new"}},
	{"Chromium, headful", "chromium", "chromium", "", map[string]any{"headless": false}},
	{"Patchright, headful", "patchright", "chromium", "", map[string]any{"headless": false}},
	// Camoufox: read the page's own world ("mw:" needs main_world_eval), like a site would.
	{"Camoufox, headful", "camoufox", "firefox", "mw:", map[string]any{"headless": false, "os": "windows", "main_world_eval": true}},
}

// The classic headless and automation tells, read the way a detection script reads them.
const signals = `(async () => {
  let query = null;
  try { query = (await navigator.permissions.query({ name: 'notifications' })).state; } catch { query = 'error'; }
  const gl = (() => {
    try { const g = document.createElement('canvas').getContext('webgl'); const d = g.getExtension('WEBGL_debug_renderer_info'); return g.getParameter(d ? d.UNMASKED_RENDERER_WEBGL : g.RENDERER); } catch { return null; }
  })();
  return {
    ua_says_headless: /Headless/.test(navigator.userAgent),
    webdriver: navigator.webdriver,
    plugins: navigator.plugins.length,
    notification_mismatch: typeof Notification !== 'undefined' && Notification.permission === 'denied' && query === 'prompt',
    outer_window_zero: outerWidth === 0 || outerHeight === 0,
    screen: screen.width + 'x' + screen.height,
    webgl_renderer: gl,
    cores: navigator.hardwareConcurrency,
    memory_gb: navigator.deviceMemory ?? null,
    languages: navigator.languages.join(','),
    chrome_object: typeof window.chrome === 'object' && window.chrome !== null,
  };
})()`

var tellKeys = []string{"ua_says_headless", "webdriver", "notification_mismatch", "outer_window_zero"}

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

func inspect(pw *playwright.Playwright, t target) map[string]any {
	fail := func(err error) map[string]any { return map[string]any{"label": t.label, "error": err.Error()} }
	options := map[string]any{"proxy": proxy}
	for k, v := range t.body {
		options[k] = v
	}
	session, err := launch(t.engine, options)
	if err != nil {
		return fail(err)
	}
	connect := pw.Chromium.Connect
	if t.family == "firefox" {
		connect = pw.Firefox.Connect
	}
	browser, err := connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fail(err)
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return fail(err)
	}
	if _, err := page.Goto("https://example.com/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return fail(err)
	}
	v, err := page.Evaluate(t.prefix + signals)
	if err != nil {
		return fail(err)
	}
	out := v.(map[string]any)
	out["label"] = t.label
	out["threads"] = num(session["weight"])
	var tells []string
	for _, k := range tellKeys {
		if b, ok := out[k].(bool); ok && b {
			tells = append(tells, k)
		}
	}
	out["tells"] = "none"
	if len(tells) > 0 {
		out["tells"] = strings.Join(tells, ", ")
	}
	return out
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	rows := make([]map[string]any, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(i int, t target) { defer wg.Done(); rows[i] = inspect(pw, t) }(i, t)
	}
	wg.Wait()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(rows)
	fmt.Print(buf.String())
}
