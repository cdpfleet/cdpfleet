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

// Every setup gets its own sticky residential exit (letter = setup): DataDome scores the IP as well as the browser,
// so one setup's visits must not change what the next one sees.
var networkLabels = map[string]string{
	"own":   "own proxy (rotating)",
	"tier1": "cdpfleet-resi Tier 1, sticky US",
	"tier4": "cdpfleet-resi Tier 4, sticky US",
}

func proxyFor(net, exit string) string {
	switch net {
	case "tier1":
		return "cdpfleet-resi-country-us-session-d" + runID + exit + "-lifetime-30"
	case "tier4":
		return "cdpfleet-resi-tier-4-country-us-session-d" + runID + exit + "-lifetime-30"
	}
	return os.Getenv("PROXY_URL")
}

type setup struct {
	ID, Name, Engine, Type string
	Headless               bool
}

var setups = map[string]setup{
	"chromium": {"a", "Chromium, headless", "chromium", "chromium", true},
	"chrome":   {"b", "Google Chrome, headful", "chrome", "chromium", false},
	"edge":     {"c", "Microsoft Edge, headful", "edge", "chromium", false},
	"camoufox": {"d", "Camoufox, defaults", "camoufox", "firefox", true},
}

// DataDome's own public site, which DataDome protects: its home page and its pricing page.
var pages = map[string]string{
	"home":    "https://datadome.co/",
	"pricing": "https://datadome.co/pricing/",
}

// What the page shows right now, in the lab's words. DataDome answers an intercepted request with a 403 page that
// embeds an iframe from captcha-delivery.com: a device check that passes by itself, a CAPTCHA, or a block (t=bv).
// The same expression runs in every language.
const state = `(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()`

type cell struct {
	Outcome string `json:"outcome"`
	Status  *int   `json:"status"`
	Seconds *int   `json:"seconds"`
	Cookie  *bool  `json:"cookie,omitempty"`
}

type row struct {
	Setup       string   `json:"setup"`
	Network     string   `json:"network"`
	Home        *cell    `json:"home,omitempty"`
	Pricing     *cell    `json:"pricing,omitempty"`
	MoveSeconds *float64 `json:"move_seconds,omitempty"`
}

var pw *playwright.Playwright

// 503 means the fleet is momentarily busy: wait Retry-After and try again. A profile's second session can land on a
// node with an older build of the browser (409 profile_version_downgrade): ask again for another node.
func launch(s setup, net, exit string, extra map[string]any) playwright.Browser {
	if exit == "" {
		exit = s.ID
	}
	body := map[string]any{"proxy": proxyFor(net, exit), "headless": s.Headless, "record": false}
	for k, v := range extra {
		body[k] = v
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
		retry := res.StatusCode == 429 || res.StatusCode == 503 || (res.StatusCode == 409 && bytes.Contains(msg, []byte("profile_version_downgrade")))
		if !retry || attempt == 8 {
			log.Fatalf("launch %d %s", res.StatusCode, msg)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func goTo(tab playwright.Page, url string) (*int, error) {
	res, err := tab.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	status := res.Status()
	return &status, nil
}

// Load a page (a network failure is retried once), then watch it once a second for 12 s without touching it.
// content = the page straight away; cleared = DataDome intercepted (403 or its device check) and let the browser
// through by itself; challenge = a CAPTCHA (never solved); blocked = DataDome's block page.
func visit(tab playwright.Page, url string) *cell {
	t0 := time.Now()
	status, err := goTo(tab, url)
	if err != nil {
		if status, err = goTo(tab, url); err != nil {
			return &cell{Outcome: "error"}
		}
	}
	checked := status != nil && *status == 403
	until := time.Now().Add(12 * time.Second) // the watch starts once the page has loaded, however slow the proxy was
	for time.Now().Before(until) {
		st := ""
		if v, err := tab.Evaluate(state); err == nil { // fails while the device check reloads the page
			st, _ = v.(string)
		}
		switch st {
		case "blocked", "challenge":
			return &cell{Outcome: st, Status: status}
		case "checking":
			checked = true
		case "page":
			if checked {
				seconds := int(math.Round(time.Since(t0).Seconds()))
				return &cell{Outcome: "cleared", Status: status, Seconds: &seconds}
			}
			return &cell{Outcome: "content", Status: status}
		}
		time.Sleep(time.Second)
	}
	if checked {
		return &cell{Outcome: "challenge", Status: status}
	}
	return &cell{Outcome: "error", Status: status}
}

func newTab(ctx playwright.BrowserContext) playwright.Page {
	tab, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	return tab
}

// One session per setup and network; a fresh context per page so every load is a first visit.
func measure(setupKey, net string, keys []string) row {
	s := setups[setupKey]
	browser := launch(s, net, "", nil)
	defer browser.Close()
	r := row{Setup: s.Name, Network: networkLabels[net]}
	for _, k := range keys {
		ctx, err := browser.NewContext()
		if err != nil {
			log.Fatal(err)
		}
		c := visit(newTab(ctx), pages[k])
		if k == "home" {
			r.Home = c
		} else {
			r.Pricing = c
		}
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

func hasCookie(ctx playwright.BrowserContext) bool {
	cookies, _ := ctx.Cookies()
	for _, c := range cookies {
		if c.Name == "datadome" {
			return true
		}
	}
	return false
}

type returnVisit struct {
	Setup   string `json:"setup"`
	Network string `json:"network"`
	First   *cell  `json:"first"`
	Second  *cell  `json:"second"`
}

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart. The profile keeps
// the datadome cookie the first visit was given.
func returning(setupKey string) returnVisit {
	s := setups[setupKey]
	name := "case65-" + runID + "-" + s.ID
	visits := []*cell{}
	for i := 0; i < 2; i++ {
		browser := launch(s, "tier1", s.ID+"p", map[string]any{"profile": name})
		ctx := browser.Contexts()[0] // the profile's own context; a NewContext() is not saved
		c := visit(newTab(ctx), pages["home"])
		// DataDome's script sets its cookie a moment after the page: give it up to 5 s.
		for j := 0; j < 5 && !hasCookie(ctx); j++ {
			time.Sleep(time.Second)
		}
		got := hasCookie(ctx)
		c.Cookie = &got
		visits = append(visits, c)
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
	return returnVisit{s.Name, networkLabels["tier1"], visits[0], visits[1]}
}

// Camoufox with humanize on and off: two page views in one context, with three mouse moves between them, so DataDome's
// script on the first page can see how the cursor moves before the second page is requested.
func humanize(on bool) row {
	exit, label := "h0", "off"
	if on {
		exit, label = "h1", "on"
	}
	browser := launch(setups["camoufox"], "tier1", exit, map[string]any{"humanize": on})
	defer browser.Close()
	ctx, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	tab := newTab(ctx)
	r := row{Setup: "Camoufox, humanize " + label, Network: networkLabels["tier1"]}
	r.Home = visit(tab, pages["home"])
	t0 := time.Now()
	for _, xy := range [][2]float64{{200, 300}, {700, 250}, {450, 600}} {
		tab.Mouse().Move(xy[0], xy[1])
	}
	moved := math.Round(time.Since(t0).Seconds()*10) / 10
	r.MoveSeconds = &moved
	r.Pricing = visit(tab, pages["pricing"])
	return r
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
			matrix = append(matrix, measure(setupKey, net, []string{"home", "pricing"}))
		}
	}
	tier4 := []row{}
	for _, setupKey := range []string{"chrome", "camoufox"} {
		tier4 = append(tier4, measure(setupKey, "tier4", []string{"home"}))
	}
	profiles := []returnVisit{returning("chrome"), returning("camoufox")}
	human := []row{humanize(true), humanize(false)}
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "matrix": matrix, "tier4": tier4, "profiles": profiles, "humanize": human}, "", "  ")
	fmt.Println(string(out))
}
