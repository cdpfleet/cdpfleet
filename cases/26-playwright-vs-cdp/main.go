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
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// What a page can notice about the client driving it. A debugger that has Runtime.enable'd
// the page serializes logged errors, which reads their stack getter.
const probe = `(async () => {
  let stackRead = false;
  const e = new Error('probe');
  Object.defineProperty(e, 'stack', { get() { stackRead = true; return ''; } });
  console.debug(e);
  await new Promise((r) => setTimeout(r, 100));
  return { stack_read_by_debugger: stackRead, webdriver: navigator.webdriver };
})()`

type row struct {
	Mode                   string `json:"mode"`
	Endpoint               string `json:"endpoint"`
	ClientVersionMustMatch bool   `json:"client_version_must_match"`
	ConnectMs              int64  `json:"connect_ms"`
	BrowserVersion         string `json:"browser_version"`
	ConsoleEvents          bool   `json:"console_events"`
	PDF                    bool   `json:"pdf"`
	StackReadByDebugger    any    `json:"stack_read_by_debugger"`
	Webdriver              any    `json:"webdriver"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func launch(cdp bool) map[string]any {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new", "cdp": cdp})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chrome/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		log.Fatalf("launch %s %s", res.Status, msg)
	}
	var s map[string]any
	must(0, json.NewDecoder(res.Body).Decode(&s))
	return s
}

func measure(pw *playwright.Playwright, mode string) row {
	pwProtocol := mode == "playwright protocol"
	s := launch(!pwProtocol)
	headers := map[string]string{"x-api-key": key}
	t := time.Now()
	var browser playwright.Browser
	if pwProtocol {
		browser = must(pw.Chromium.Connect(s["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: headers}))
	} else {
		browser = must(pw.Chromium.ConnectOverCDP(s["cdpUrl"].(string), playwright.BrowserTypeConnectOverCDPOptions{Headers: headers}))
	}
	connectMs := time.Since(t).Milliseconds()
	defer browser.Close()

	page := must(browser.NewPage())
	var mu sync.Mutex
	logged := map[string]bool{}
	page.OnConsole(func(m playwright.ConsoleMessage) {
		mu.Lock()
		logged[m.Type()] = true
		mu.Unlock()
	})
	for attempt := 1; ; attempt++ { // the proxy can drop a tunnel; retry
		_, err := page.Goto("https://example.com/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil {
			break
		}
		if attempt == 3 {
			log.Fatal(err)
		}
	}
	seen := must(page.Evaluate(probe)).(map[string]any)
	page.WaitForTimeout(200) // let the console event arrive
	pdf := false
	if b, err := page.PDF(); err == nil { // not over every connection
		pdf = len(b) > 0
	}
	mu.Lock()
	sawDebug := logged["debug"]
	mu.Unlock()
	return row{
		Mode: mode, Endpoint: map[bool]string{true: "wsUrl", false: "cdpUrl"}[pwProtocol], ClientVersionMustMatch: pwProtocol,
		ConnectMs: connectMs, BrowserVersion: browser.Version(), ConsoleEvents: sawDebug, PDF: pdf,
		StackReadByDebugger: seen["stack_read_by_debugger"], Webdriver: seen["webdriver"],
	}
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	out := []row{measure(pw, "playwright protocol"), measure(pw, "connectOverCDP")}
	text, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(text))
}
