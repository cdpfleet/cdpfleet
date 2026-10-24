// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

type network struct{ Label, Proxy string }

// The sticky residential exit is shared by every setup, so a difference between rows is the browser.
var networks = map[string]network{
	"own":   {"own proxy (rotating)", os.Getenv("PROXY_URL")},
	"tier1": {"cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c68" + runID + "-lifetime-30"},
}

type setup struct {
	Name, Engine, Type string
	Headless           bool
}

var setups = []setup{
	{"Chromium, headless", "chromium", "chromium", true},
	{"Google Chrome, headful", "chrome", "chromium", false},
	{"Microsoft Edge, headful", "edge", "chromium", false},
	{"Brave, headful", "brave", "chromium", false},
	{"Camoufox, defaults", "camoufox", "firefox", true},
	{"Firefox, headful", "firefox", "firefox", false},
	{"WebKit, headful", "webkit", "webkit", false},
}

// Vendors' own public demo pages. Google's v3 demo asks for a score as soon as it loads and prints its
// server's verify response (with the score) on the page; hCaptcha's demo shows a checkbox widget.
const recaptchaURL = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php"
const hcaptchaURL = "https://accounts.hcaptcha.com/demo"

// Read what the page shows; the same expressions run in every language.
const scoreJS = `(() => { const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()`
const hcWidgetJS = `(() => { const f = document.querySelector('iframe[src*="hcaptcha.com"]'); return f ? (f.getBoundingClientRect().height > 0) : false; })()`

type rcCell struct {
	Outcome string   `json:"outcome"`
	Score   *float64 `json:"score"`
}
type hcCell struct {
	Outcome string `json:"outcome"`
}
type row struct {
	Setup     string `json:"setup"`
	Network   string `json:"network"`
	Recaptcha rcCell `json:"recaptcha"`
	Hcaptcha  hcCell `json:"hcaptcha"`
}

var pw *playwright.Playwright

func launch(s setup, net string) (playwright.Browser, error) {
	proxy := networks[net].Proxy
	body := map[string]any{"proxy": proxy, "headless": s.Headless, "record": false}
	// WebKit takes an authenticated HTTP proxy through the updatable route (see the WebKit page).
	if s.Engine == "webkit" && regexp.MustCompile(`^https?:`).MatchString(proxy) {
		body["proxy_updatable"] = true
	}
	payload, _ := json.Marshal(body)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+s.Engine+"/session", bytes.NewReader(payload))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session struct{ WsURL string }
			json.Unmarshal(msg, &session)
			bt := pw.Chromium
			if s.Type == "firefox" {
				bt = pw.Firefox
			} else if s.Type == "webkit" {
				bt = pw.WebKit
			}
			return bt.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			return nil, errors.New("launch " + strconv.Itoa(res.StatusCode) + " " + string(msg))
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func open(tab playwright.Page, url string) bool {
	_, err := tab.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)})
	return err == nil
}

// One load; then wait up to 20 s for the score the demo prints. Nothing on the page is touched.
func recaptcha(ctx playwright.BrowserContext) rcCell {
	tab, err := ctx.NewPage()
	if err != nil {
		return rcCell{Outcome: "error"}
	}
	defer tab.Close()
	if !open(tab, recaptchaURL) {
		return rcCell{Outcome: "error"}
	}
	for i := 0; i < 20; i++ {
		time.Sleep(time.Second)
		v, err := tab.Evaluate(scoreJS)
		if err != nil || v == nil {
			continue
		}
		var score float64
		switch n := v.(type) {
		case float64:
			score = n
		case int:
			score = float64(n)
		default:
			continue
		}
		return rcCell{Outcome: "score", Score: &score}
	}
	return rcCell{Outcome: "no score"}
}

// hCaptcha only decides when someone clicks its checkbox, so a load shows whether the widget renders, nothing more.
func hcaptcha(ctx playwright.BrowserContext) hcCell {
	tab, err := ctx.NewPage()
	if err != nil {
		return hcCell{Outcome: "error"}
	}
	defer tab.Close()
	if !open(tab, hcaptchaURL) {
		return hcCell{Outcome: "error"}
	}
	for i := 0; i < 12; i++ {
		time.Sleep(time.Second)
		if v, err := tab.Evaluate(hcWidgetJS); err == nil && v == true {
			return hcCell{Outcome: "widget shown"}
		}
	}
	return hcCell{Outcome: "no widget"}
}

func measure(s setup, net string) row {
	r := row{Setup: s.Name, Network: networks[net].Label}
	browser, err := launch(s, net)
	if err != nil {
		r.Recaptcha, r.Hcaptcha = rcCell{Outcome: "error"}, hcCell{Outcome: "error"}
		return r
	}
	defer browser.Close()
	// A session the server ends (e.g. its memory limit) leaves the remaining cells as errors.
	r.Recaptcha, r.Hcaptcha = rcCell{Outcome: "error"}, hcCell{Outcome: "error"}
	if ctx, err := browser.NewContext(); err == nil { // a fresh context: every load is a first visit
		r.Recaptcha = recaptcha(ctx)
		ctx.Close()
	}
	if ctx, err := browser.NewContext(); err == nil {
		r.Hcaptcha = hcaptcha(ctx)
		ctx.Close()
	}
	return r
}

func main() {
	var err error
	pw, err = playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	rows := []row{}
	for _, net := range []string{"own", "tier1"} {
		for _, s := range setups {
			rows = append(rows, measure(s, net))
		}
	}
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "rows": rows}, "", "  ")
	fmt.Println(string(out))
}
