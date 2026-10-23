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
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

type network struct{ Label, Proxy string }

// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
var networks = map[string]network{
	"own":   {"own proxy (rotating)", os.Getenv("PROXY_URL")},
	"tier1": {"cdpfleet-resi Tier 1, sticky US", "cdpfleet-resi-country-us-session-c67" + runID + "-lifetime-30"},
	"tier4": {"cdpfleet-resi Tier 4, sticky US", "cdpfleet-resi-tier-4-country-us-session-c67" + runID + "-lifetime-30"},
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

var pxHost = regexp.MustCompile(`(^|\.)px-(cloud|cdn)\.net$`)

type testPage struct {
	URL    string
	Sensor func(host string) bool
}

// Each vendor's own home page, which that vendor protects with its own product.
var pages = map[string]testPage{
	"akamai": {"https://www.akamai.com/", func(h string) bool { return h == "www.akamai.com" }},
	"human":  {"https://www.humansecurity.com/", func(h string) bool { return pxHost.MatchString(h) }},
}
var pageOrder = []string{"akamai", "human"}

// What the page shows right now, in the lab's words. The same expression runs in every language.
const state = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const blocked = /access denied|you don't have permission to access|request blocked|reference #\d/i.test(t);
  const challenge = !!document.querySelector('#px-captcha, #sec-if-cpt-container, #sec-cpt-if') || /press (&|and) hold|challenge validation/i.test(t);
  return blocked ? 'blocked' : challenge ? 'challenge' : 'page';
})()`

type cell struct {
	Outcome     string  `json:"outcome"`
	Status      *int    `json:"status,omitempty"`
	SensorPosts *int    `json:"sensor_posts,omitempty"`
	AbckAtLoad  *string `json:"abck_at_load,omitempty"`
	AbckAfter   *string `json:"abck_after,omitempty"`
	Px3         *bool   `json:"px3,omitempty"`
}

type row struct {
	Setup   string `json:"setup"`
	Network string `json:"network"`
	Akamai  *cell  `json:"akamai,omitempty"`
	Human   *cell  `json:"human,omitempty"`
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

// Akamai's _abck carries a status between its first two "~": -1 until a sensor post is accepted. Only that
// field is kept, never the cookie itself. HUMAN issues _px3 once its collector accepts the browser.
func signals(ctx playwright.BrowserContext) (*string, bool) {
	cookies, _ := ctx.Cookies()
	var abck *string
	px3 := false
	for _, c := range cookies {
		if c.Name == "_abck" {
			parts := strings.Split(c.Value, "~")
			v := "?"
			if len(parts) > 1 {
				v = parts[1]
			}
			abck = &v
		}
		if c.Name == "_px3" {
			px3 = true
		}
	}
	return abck, px3
}

func pageState(tab playwright.Page) string {
	v, err := tab.Evaluate(state) // fails while the page navigates
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// One load, then watch the page once a second for 10 s without touching it, so the vendor's script
// can post its sensor data; a network failure is retried once.
func visit(ctx playwright.BrowserContext, k string) *cell {
	page := pages[k]
	tab, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	var posts int64
	tab.OnRequest(func(r playwright.Request) {
		if u, err := url.Parse(r.URL()); err == nil && r.Method() == "POST" && page.Sensor(u.Hostname()) {
			atomic.AddInt64(&posts, 1)
		}
	})
	nav := playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)}
	res, err := tab.Goto(page.URL, nav)
	if err != nil {
		if res, err = tab.Goto(page.URL, nav); err != nil {
			tab.Close()
			return &cell{Outcome: "error"}
		}
	}
	var status *int
	if res != nil {
		s := res.Status()
		status = &s
	}
	atLoad, _ := signals(ctx)
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		s := pageState(tab)
		if s != "" {
			seen[s] = true
		}
		if s == "blocked" {
			break
		}
		time.Sleep(time.Second)
	}
	last := pageState(tab)
	outcome := "content"
	switch {
	case seen["blocked"] || last == "blocked":
		outcome = "blocked"
	case last == "challenge":
		outcome = "challenge"
	case seen["challenge"]:
		outcome = "cleared"
	}
	after, px3 := signals(ctx)
	tab.Close()
	n := int(atomic.LoadInt64(&posts))
	c := &cell{Outcome: outcome, Status: status, SensorPosts: &n}
	if k == "akamai" {
		c.AbckAtLoad, c.AbckAfter = atLoad, after
	} else {
		c.Px3 = &px3
	}
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
		c := visit(ctx, k)
		if k == "akamai" {
			r.Akamai = c
		} else {
			r.Human = c
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

type returnVisit struct {
	Setup   string `json:"setup"`
	Network string `json:"network"`
	Page    string `json:"page"`
	First   *cell  `json:"first"`
	Second  *cell  `json:"second"`
}

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
func returning(setupKey, net, k string) returnVisit {
	name := "case67-" + runID
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
		tier4 = append(tier4, measure(setupKey, "tier4", pageOrder))
	}
	profile := returning("chrome", "tier1", "akamai")
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "matrix": matrix, "tier4": tier4, "profile": profile}, "", "  ")
	fmt.Println(string(out))
}
