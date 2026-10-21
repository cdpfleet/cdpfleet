// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

type network struct{ Label, Proxy string }

// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
var networks = map[string]network{
	"own":   {"own proxy (rotating)", os.Getenv("PROXY_URL")},
	"tier1": {"cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c64" + runID + "-lifetime-30"},
	"tier4": {"cdpfleet-resi Tier 4, sticky US", "cdpfleet-resi-tier-4-country-us-session-c64" + runID + "-lifetime-30"},
}

type setup struct {
	Name, Engine, Type string
	Headless           bool
}

var setups = map[string]setup{
	"chromium": {"Chromium, headless", "chromium", "chromium", true},
	"chrome":   {"Google Chrome, headful", "chrome", "chromium", false},
	"edge":     {"Microsoft Edge, headful", "edge", "chromium", false},
	"camoufox": {"Camoufox, defaults", "camoufox", "firefox", true},
}

type testPage struct{ URL, Kind string }

// Public test pages only: two Cloudflare challenge pages and a Turnstile test page in two widget modes.
// "cf" pages may answer with Cloudflare's interstitial; "widget" pages embed Turnstile and get a token if it passes.
var pages = map[string]testPage{
	"nowsecure":         {"https://nowsecure.nl/", "cf"},
	"scrapingcourse":    {"https://www.scrapingcourse.com/cloudflare-challenge", "cf"},
	"ts_noninteractive": {"https://peet.ws/turnstile-test/non-interactive.html", "widget"},
	"ts_managed":        {"https://peet.ws/turnstile-test/managed.html", "widget"},
}
var pageOrder = []string{"nowsecure", "scrapingcourse", "ts_noninteractive", "ts_managed"}

// What the page shows right now, in the lab's words. The same expression runs in every language.
const state = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const el = document.querySelector('[name="cf-turnstile-response"]');
  const state = /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
  return { state, token: el && el.value ? el.value.length : 0 };
})()`

type cell struct {
	Outcome     string `json:"outcome"`
	Seconds     *int   `json:"seconds"`
	CfClearance *bool  `json:"cf_clearance,omitempty"`
	Token       *bool  `json:"token,omitempty"`
}

type row struct {
	Setup            string `json:"setup"`
	Network          string `json:"network"`
	Nowsecure        *cell  `json:"nowsecure,omitempty"`
	Scrapingcourse   *cell  `json:"scrapingcourse,omitempty"`
	TsNoninteractive *cell  `json:"ts_noninteractive,omitempty"`
	TsManaged        *cell  `json:"ts_managed,omitempty"`
}

func (r *row) set(k string, c *cell) {
	switch k {
	case "nowsecure":
		r.Nowsecure = c
	case "scrapingcourse":
		r.Scrapingcourse = c
	case "ts_noninteractive":
		r.TsNoninteractive = c
	case "ts_managed":
		r.TsManaged = c
	}
}

var pw *playwright.Playwright

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(s setup, net, profile string) playwright.Browser {
	body := map[string]any{"proxy": networks[net].Proxy, "headless": s.Headless, "record": false}
	if profile != "" {
		body["profile"] = profile
	}
	payload, _ := json.Marshal(body)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+s.Engine+"/session", bytes.NewReader(payload))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session struct{ WsURL string }
			json.Unmarshal(msg, &session)
			bt := pw.Chromium
			if s.Type == "firefox" {
				bt = pw.Firefox
			}
			browser, err := bt.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
			if err != nil {
				log.Fatal(err)
			}
			return browser
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			log.Fatalf("launch %d %s", res.StatusCode, msg)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func hasClearance(ctx playwright.BrowserContext) bool {
	cookies, _ := ctx.Cookies()
	for _, c := range cookies {
		if c.Name == "cf_clearance" {
			return true
		}
	}
	return false
}

// One load, then watch the page once a second without touching it: an interstitial gets 20 s to
// clear by itself, a Turnstile widget 12 s to issue a token. A network failure is retried once.
func visit(ctx playwright.BrowserContext, k string) *cell {
	page := pages[k]
	tab, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	t0 := time.Now()
	c := &cell{Outcome: "challenge"}
	nav := playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)}
	if _, err := tab.Goto(page.URL, nav); err != nil {
		if _, err := tab.Goto(page.URL, nav); err != nil {
			tab.Close()
			return &cell{Outcome: "error"}
		}
	}
	sawChallenge := false
	limit := 20 * time.Second
	if page.Kind == "widget" {
		limit = 12 * time.Second
	}
	for time.Since(t0) < limit {
		st, token := "", 0
		if v, err := tab.Evaluate(state); err == nil { // fails while the interstitial reloads the page
			m := v.(map[string]any)
			st, _ = m["state"].(string)
			switch n := m["token"].(type) {
			case int:
				token = n
			case float64:
				token = int(n)
			}
		}
		seconds := int(math.Round(time.Since(t0).Seconds()))
		if st == "blocked" {
			c.Outcome = "blocked"
			break
		}
		if st == "challenge" {
			sawChallenge = true
		}
		if st != "" && page.Kind == "widget" && token > 0 {
			c.Outcome, c.Seconds = "cleared", &seconds
			break
		}
		if st == "page" && page.Kind == "cf" {
			c.Outcome = "content"
			if sawChallenge {
				c.Outcome, c.Seconds = "cleared", &seconds
			}
			break
		}
		time.Sleep(time.Second)
	}
	if page.Kind == "cf" {
		// The clearance cookie can arrive a moment after the content: give it up to 6 s.
		for i := 0; i < 6 && c.Outcome != "blocked" && !hasClearance(ctx); i++ {
			time.Sleep(time.Second)
		}
		cf := hasClearance(ctx)
		c.CfClearance = &cf
	} else {
		tok := c.Outcome == "cleared"
		c.Token = &tok
	}
	tab.Close()
	return c
}

// One session per setup and network; a fresh context per page so every load is a first visit.
func measure(setupKey, net string, keys []string) row {
	s := setups[setupKey]
	browser := launch(s, net, "")
	defer browser.Close()
	r := row{Setup: s.Name, Network: networks[net].Label}
	for _, k := range keys {
		ctx, err := browser.NewContext()
		if err != nil {
			log.Fatal(err)
		}
		r.set(k, visit(ctx, k))
		ctx.Close()
	}
	return r
}

func api(method, path string) map[string]any {
	req, _ := http.NewRequest(method, "https://cdpfleet.com"+path, nil)
	req.Header.Set("x-api-key", key)
	out := map[string]any{}
	if res, err := http.DefaultClient.Do(req); err == nil {
		json.NewDecoder(res.Body).Decode(&out)
		res.Body.Close()
	}
	return out
}

func profileInfo(name string) map[string]any {
	list, _ := api("GET", "/v1/me/profiles")["profiles"].([]any)
	for _, p := range list {
		if m := p.(map[string]any); m["name"] == name {
			return m
		}
	}
	return nil
}

type returnVisit struct {
	Setup   string `json:"setup"`
	Network string `json:"network"`
	Page    string `json:"page"`
	First   *cell  `json:"first"`
	Second  *cell  `json:"second"`
}

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
func returning(setupKey, net, k string) returnVisit {
	name := "case64-" + runID
	visits := []*cell{}
	for i := 0; i < 2; i++ {
		browser := launch(setups[setupKey], net, name)
		visits = append(visits, visit(browser.Contexts()[0], k)) // the profile's own context; a NewContext() is not saved
		browser.Close()
		// The node saves the profile after the browser exits: wait until it is free again.
		for j := 0; j < 60; j++ {
			if info := profileInfo(name); info != nil && info["in_use"] == false {
				break
			}
			time.Sleep(2 * time.Second)
		}
	}
	api("DELETE", "/v1/me/profiles/"+name)
	return returnVisit{setups[setupKey].Name, networks[net].Label, k, visits[0], visits[1]}
}

func main() {
	var err error
	pw, err = playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	matrix := []row{}
	for _, net := range []string{"own", "tier1"} {
		for _, setupKey := range []string{"chromium", "chrome", "edge", "camoufox"} {
			matrix = append(matrix, measure(setupKey, net, pageOrder))
		}
	}
	tier4 := []row{}
	for _, setupKey := range []string{"chrome", "camoufox"} {
		tier4 = append(tier4, measure(setupKey, "tier4", []string{"nowsecure", "scrapingcourse", "ts_managed"}))
	}
	profile := returning("camoufox", "tier1", "scrapingcourse")
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "matrix": matrix, "tier4": tier4, "profile": profile}, "", "  ")
	fmt.Println(string(out))
}
