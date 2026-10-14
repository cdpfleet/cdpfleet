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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const site = "https://books.toscrape.com/"

type setup struct{ label, engine, family, api string }

var setups = []setup{
	{"Chrome", "chrome", "chromium", "typeof chrome !== 'undefined' && !!chrome.runtime"},
	{"Firefox", "firefox", "firefox", "typeof browser !== 'undefined' && !!browser.runtime"},
}

// What an extension's content script does: runs in every page before the page's own scripts.
const contentScript = `
  window.__myExtension = true;
  document.addEventListener('DOMContentLoaded', () => {
    const b = document.createElement('div');
    b.id = 'my-extension-banner';
    b.textContent = 'Injected on ' + location.pathname;
    document.body.prepend(b);
  });`

// Field order matches the other languages' output.
type result struct {
	Label              string `json:"label"`
	LaunchStatus       int    `json:"launch_status"`
	ArgsApplied        bool   `json:"args_applied"`
	ExtensionAPI       bool   `json:"extension_api"`
	ContentScriptPages int    `json:"content_script_pages"`
	BlockedImages      int    `json:"blocked_images"`
	HeaderAdded        bool   `json:"header_added"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, opts map[string]any) (int, map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, nil, err
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session map[string]any
			return res.StatusCode, session, json.Unmarshal(b, &session)
		}
		if res.StatusCode != 503 || attempt == 5 {
			return 0, nil, fmt.Errorf("launch %s %d %s", engine, res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func one(pw *playwright.Playwright, s setup) (result, error) {
	out := result{Label: s.label}
	// Try to load an extension the way a local launch would: Chromium flags and an "extensions" list.
	status, session, err := launch(s.engine, map[string]any{
		"proxy":      os.Getenv("PROXY_URL"),
		"args":       []string{"--load-extension=/tmp/my-extension", "--user-agent=ARGS-WERE-APPLIED"},
		"extensions": []string{"/tmp/my-extension"},
	})
	if err != nil {
		return out, err
	}
	out.LaunchStatus = status
	browserType := pw.Chromium
	if s.family == "firefox" {
		browserType = pw.Firefox
	}
	browser, err := browserType.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		return out, err
	}
	ua, err := page.Evaluate("navigator.userAgent")
	if err != nil {
		return out, err
	}
	out.ArgsApplied = strings.Contains(fmt.Sprint(ua), "ARGS-WERE-APPLIED")
	// chrome.runtime / browser.runtime exist in a page only when an extension talks to it.
	api, err := page.Evaluate(s.api)
	if err != nil {
		return out, err
	}
	out.ExtensionAPI = api == true
	page.Close()

	// The Playwright equivalents, in one context.
	context, err := browser.NewContext()
	if err != nil {
		return out, err
	}
	if err := context.AddInitScript(playwright.Script{Content: playwright.String(contentScript)}); err != nil { // content script
		return out, err
	}
	if err := context.SetExtraHTTPHeaders(map[string]string{"x-my-extension": "on"}); err != nil { // header rewriting
		return out, err
	}
	blocked := 0 // request blocking, like an ad or image blocker
	if err := context.Route(regexp.MustCompile(`\.(png|jpe?g|gif|webp)(\?|$)`), func(route playwright.Route) {
		blocked++
		route.Abort()
	}); err != nil {
		return out, err
	}
	p, err := context.NewPage()
	if err != nil {
		return out, err
	}
	for _, url := range []string{site, site + "catalogue/page-2.html"} {
		if _, err := p.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
			return out, err
		}
		if n, err := p.Locator("#my-extension-banner").Count(); err == nil && n > 0 {
			out.ContentScriptPages++
		}
	}
	out.BlockedImages = blocked
	// httpbin echoes the request headers; read the response body, not the rendered page.
	resp, err := p.Goto("https://httpbin.org/headers", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		return out, err
	}
	var echoed struct {
		Headers map[string]string `json:"headers"`
	}
	if err := resp.JSON(&echoed); err != nil {
		return out, err
	}
	out.HeaderAdded = echoed.Headers["X-My-Extension"] == "on"
	return out, nil
}

// run returns errors instead of exiting, so deferred Close and Stop calls still run.
func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	results := []result{}
	for _, s := range setups {
		r, err := one(pw, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
		results = append(results, r)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"results": results}); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
