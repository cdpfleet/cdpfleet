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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL")

// Six ways to drive a remote browser. Chromium (Playwright's build) and Patchright share a
// Chromium base, so they're the like-for-like pair; Chrome is also driven over CDP, with and
// without a console listener; Camoufox is Firefox, driven over Playwright's Juggler, not CDP.
type setup struct {
	label, engine, family string
	cdp, listen           bool
}

var setups = []setup{
	{"Chromium, Playwright connect", "chromium", "chromium", false, true},
	{"Patchright, Playwright connect", "patchright", "chromium", false, true},
	{"Chrome, Playwright connect", "chrome", "chromium", false, true},
	{"Chrome, connectOverCDP", "chrome", "chromium", true, true},
	{"Chrome, connectOverCDP, no console listener", "chrome", "chromium", true, false},
	{"Camoufox, Playwright connect", "camoufox", "firefox", false, true},
}

// What a detection script can do from inside the page, run as the page's own script:
//   - the classic tell: log an Error whose `stack` is a getter; a debugger client that previews
//     logged objects used to read it;
//   - an Error whose `message` is a getter, the same idea;
//   - the cost of console.log: with a client listening to the console, every logged object is
//     serialized for it. Logging a big object 40 times is timed against JSON.stringify of the
//     same object (so the machine's speed cancels out), five times; the median ratio is kept.
//
// The verdict is left in an attribute that every client can read.
const page = `<html><body><p>console probe</p><script>
  const out = { stack_getter_read: false, message_getter_read: false };
  const e1 = new Error('probe');
  Object.defineProperty(e1, 'stack', { get() { out.stack_getter_read = true; return ''; } });
  console.debug(e1);
  const e2 = new Error('probe');
  Object.defineProperty(e2, 'message', { get() { out.message_getter_read = true; return ''; } });
  console.log(e2);
  console.log('hello-from-page');
  const big = Array.from({ length: 1000 }, (_, i) => ({ i, s: 'x'.repeat(40), a: [1, 2, 3, { d: i }] }));
  const ratios = [];
  for (let k = 0; k < 5; k++) {
    let t = performance.now();
    for (let i = 0; i < 40; i++) console.log(big);
    const logMs = performance.now() - t;
    t = performance.now();
    for (let i = 0; i < 40; i++) JSON.stringify(big);
    ratios.push(logMs / (performance.now() - t));
  }
  ratios.sort((a, b) => a - b);
  out.log_cost_ratio = Math.round(ratios[2] * 100) / 100;
  setTimeout(() => document.documentElement.setAttribute('data-out', JSON.stringify(out)), 1000);
</script></body></html>`

// Field order matches the other languages' output.
type result struct {
	Label             string  `json:"label"`
	Error             string  `json:"error,omitempty"`
	Engine            string  `json:"engine,omitempty"`
	Connect           string  `json:"connect,omitempty"`
	ConsoleListener   bool    `json:"console_listener"`
	ConsoleEvents     *int    `json:"console_events"`
	StackGetterRead   bool    `json:"stack_getter_read"`
	MessageGetterRead bool    `json:"message_getter_read"`
	LogCostRatio      float64 `json:"log_cost_ratio"`
	EvaluateWorks     bool    `json:"evaluate_works"`
}

func launch(engine string, cdp bool) (map[string]any, error) {
	opts := map[string]any{"proxy": proxy, "headless": true, "record": false}
	if cdp {
		opts["cdp"] = true
	}
	body, _ := json.Marshal(opts)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch %d %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

func probe(pw *playwright.Playwright, s setup) (result, error) {
	out := result{Label: s.label}
	session, err := launch(s.engine, s.cdp)
	if err != nil {
		out.Error = err.Error()
		return out, nil
	}
	browserType := pw.Chromium
	if s.family == "firefox" {
		browserType = pw.Firefox
	}
	headers := map[string]string{"x-api-key": key}
	var browser playwright.Browser
	if s.cdp {
		browser, err = browserType.ConnectOverCDP(session["cdpUrl"].(string), playwright.BrowserTypeConnectOverCDPOptions{Headers: headers})
	} else {
		browser, err = browserType.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: headers})
	}
	if err != nil {
		return out, err
	}
	defer browser.Close()
	var context playwright.BrowserContext
	if s.cdp {
		context = browser.Contexts()[0]
	} else if context, err = browser.NewContext(); err != nil {
		return out, err
	}
	pg, err := context.NewPage()
	if err != nil {
		return out, err
	}
	events := 0
	if s.listen {
		pg.OnConsole(func(m playwright.ConsoleMessage) {
			if strings.Contains(m.Text(), "hello-from-page") {
				events++
			}
		})
	}
	if err := pg.SetContent(page); err != nil {
		return out, err
	}
	if _, err := pg.WaitForSelector("html[data-out]", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(30000)}); err != nil {
		return out, err
	}
	raw, err := pg.Evaluate("() => document.documentElement.getAttribute('data-out')")
	if err != nil {
		return out, err
	}
	var p struct {
		StackGetterRead   bool    `json:"stack_getter_read"`
		MessageGetterRead bool    `json:"message_getter_read"`
		LogCostRatio      float64 `json:"log_cost_ratio"`
	}
	if err := json.Unmarshal([]byte(raw.(string)), &p); err != nil {
		return out, err
	}
	answer, err := pg.Evaluate("() => 6 * 7")
	if err != nil {
		return out, err
	}
	out.Engine, out.ConsoleListener = s.engine, s.listen
	out.Connect = "connect"
	if s.cdp {
		out.Connect = "connectOverCDP"
	}
	if s.listen {
		out.ConsoleEvents = &events
	}
	out.StackGetterRead, out.MessageGetterRead, out.LogCostRatio = p.StackGetterRead, p.MessageGetterRead, p.LogCostRatio
	out.EvaluateWorks = fmt.Sprint(answer) == "42"
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
	for _, s := range setups { // one at a time: an idle session ends after 2 minutes by default
		r, err := probe(pw, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
		results = append(results, r)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"setups": results}); err != nil {
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
