// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, LAB_SITES_KASADA_AWSWAF (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
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
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

type setup struct {
	ID, Name, Engine, Type string
	Headless               any // false, true or "new"
}

// Every browser the fleet offers that a Playwright 1.60 client can drive (Rebrowser needs 1.52, so it's left out).
var setups = []setup{
	{"chrome_headful", "Chrome, headful", "chrome", "chromium", false},
	{"chrome_headless", "Chrome, headless", "chrome", "chromium", "new"},
	{"edge", "Edge, headful", "edge", "chromium", false},
	{"brave", "Brave, headful", "brave", "chromium", false},
	{"opera", "Opera, headful", "opera", "chromium", false},
	{"yandex", "Yandex, headful", "yandex", "chromium", false},
	{"whale", "Whale, headful", "whale", "chromium", false},
	{"chromium_headless", "Chromium, headless", "chromium", "chromium", true},
	{"patchright", "Patchright, headful", "patchright", "chromium", false},
	{"firefox", "Firefox, headful", "firefox", "firefox", false},
	{"webkit", "WebKit, headful", "webkit", "webkit", false},
	{"camoufox", "Camoufox, headful", "camoufox", "firefox", false},
}

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words. AWS WAF's
// CAPTCHA ("Let's confirm you are human") is interactive: a challenge. A near-empty page 8 s later
// (Kasada's 429 answer is a blank page with a script) or an "access denied" page is blocked.
const state = `(() => {
  const text = document.body ? document.body.innerText : '';
  const t = (document.title + ' ' + text.slice(0, 600)).replace(/\s+/g, ' ');
  if (document.querySelector('#captcha-container, awswaf-captcha') || (/confirm you are human|verify you are (a )?human|are you a robot|solve the puzzle/i.test(t) && text.length < 4000)) return 'challenge';
  if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request (unsuccessful|blocked)|too many requests/i.test(t) && text.length < 4000) return 'blocked';
  if (text.trim().length < 200) return 'blank';
  return 'content';
})()`

var kasadaURL = regexp.MustCompile(`/ips\.js(\?|$)|/(tl|fp)(\?|$)`)
var awsURL = regexp.MustCompile(`awswaf\.com|/challenge\.js(\?|$)`)
var vendorCookie = regexp.MustCompile(`(?i)^(KP_|aws-waf)`)

type cell struct {
	Outcome     string   `json:"outcome"`
	Status      int      `json:"status"`
	FinalStatus int      `json:"final_status"`
	ClearedS    *float64 `json:"cleared_s,omitempty"`
	Kasada      bool     `json:"kasada"`
	AwsWaf      bool     `json:"aws_waf"`
	WafAction   string   `json:"waf_action,omitempty"`
	Cookies     []string `json:"cookies"`
}

func failed(outcome string) cell { return cell{Outcome: outcome, Cookies: []string{}} }

func hasPrefix(h map[string]string, p string) bool {
	for n := range h {
		if strings.HasPrefix(strings.ToLower(n), p) {
			return true
		}
	}
	return false
}

func api(method, path string) map[string]any {
	req, _ := http.NewRequest(method, "https://cdpfleet.com"+path, nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return map[string]any{}
	}
	defer res.Body.Close()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return out
}

func launch(engine string, headless any, proxy, profile string) (string, error) {
	body := map[string]any{"proxy": proxy, "headless": headless, "record": false}
	if profile != "" {
		body["profile"] = profile
	}
	b, _ := json.Marshal(body)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(b))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var s struct{ WsUrl string }
			json.Unmarshal(data, &s)
			return s.WsUrl, nil
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			return "", fmt.Errorf("launch %d %s", res.StatusCode, data)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// One session, one load. Records the status of the page's first response and of the last main-frame
// document 8 s later (an interstitial that clears itself reloads the page), which vendor's scripts,
// headers or cookies showed up (cookie and header NAMES only; values are never read), and the state.
func visit(pw *playwright.Playwright, s setup, url, proxy, profile string) cell {
	ws, err := launch(s.Engine, s.Headless, proxy, profile)
	if err != nil {
		return failed("error")
	}
	bt := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}[s.Type]
	browser, err := bt.Connect(ws, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return failed("error")
	}
	defer browser.Close()
	var ctx playwright.BrowserContext
	if profile != "" {
		ctx = browser.Contexts()[0]
	} else if ctx, err = browser.NewContext(); err != nil {
		return failed("error")
	}
	tab, err := ctx.NewPage()
	if err != nil {
		return failed("error")
	}
	var mu sync.Mutex
	c := cell{Cookies: []string{}}
	type doc struct {
		status int
		at     float64
	}
	docs := []doc{}
	t0 := time.Now()
	main := tab.MainFrame()
	tab.OnRequest(func(r playwright.Request) {
		mu.Lock()
		defer mu.Unlock()
		if kasadaURL.MatchString(r.URL()) || hasPrefix(r.Headers(), "x-kpsdk") {
			c.Kasada = true
		}
		if awsURL.MatchString(r.URL()) {
			c.AwsWaf = true
		}
	})
	tab.OnResponse(func(r playwright.Response) {
		h := r.Headers()
		mu.Lock()
		defer mu.Unlock()
		if hasPrefix(h, "x-kpsdk") {
			c.Kasada = true
		}
		if a := h["x-amzn-waf-action"]; a != "" {
			c.AwsWaf = true
			if (a == "challenge" || a == "captcha") && c.WafAction == "" {
				c.WafAction = a
			}
		}
		if r.Request().IsNavigationRequest() && r.Frame() == main {
			docs = append(docs, doc{r.Status(), time.Since(t0).Seconds()})
		}
	})
	res, err := tab.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)})
	if err != nil {
		// A reset HTTP/2 connection is a refusal, not a network failure.
		if strings.Contains(err.Error(), "ERR_HTTP2_PROTOCOL_ERROR") {
			return failed("blocked")
		}
		return failed("error")
	}
	status := 0
	if res != nil {
		status = res.Status()
	}
	tab.WaitForTimeout(8000)
	v, err := tab.Evaluate(state)
	outcome, _ := v.(string)
	if err != nil || outcome == "" {
		outcome = "error"
	}
	// Read the cookies before taking the lock: the event handlers need it, and a Playwright call made
	// while holding it would wait on the very goroutine that delivers those events.
	cookies, cookieErr := ctx.Cookies()
	mu.Lock()
	defer mu.Unlock()
	final, finalAt := status, 0.0
	if len(docs) > 0 {
		final, finalAt = docs[len(docs)-1].status, docs[len(docs)-1].at
	}
	// A 202/429 check that passed by itself reloads the page with a success status: cleared, even if
	// the new page hasn't rendered yet at the 8 s mark. Without that reload it never cleared.
	if outcome == "content" || outcome == "blank" {
		if final >= 400 {
			outcome = "blocked"
		} else if status >= 400 || status == 202 {
			if len(docs) > 1 && final != 202 {
				outcome = "cleared"
				s := math.Round(finalAt*10) / 10
				c.ClearedS = &s
			} else {
				outcome = "blocked"
			}
		} else if outcome == "blank" {
			outcome = "blocked"
		}
	}
	c.Outcome, c.Status, c.FinalStatus = outcome, status, final
	if cookieErr == nil {
		set := map[string]bool{}
		for _, k := range cookies {
			if vendorCookie.MatchString(k.Name) && !set[k.Name] {
				set[k.Name] = true
				c.Cookies = append(c.Cookies, k.Name)
				if strings.HasPrefix(strings.ToUpper(k.Name), "KP_") {
					c.Kasada = true
				} else {
					c.AwsWaf = true
				}
			}
		}
		sort.Strings(c.Cookies)
	}
	return c
}

// One sticky residential exit per site; its setups run one after another (one session at a time per
// site), and the sites run side by side. A network error gets one more try.
func site(label, url string) map[string]any {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	proxy := "cdpfleet-resi-country-us-session-c77" + runID + label + "-lifetime-30"
	results := map[string]cell{}
	for _, s := range setups {
		r := visit(pw, s, url, proxy, "")
		if r.Outcome == "error" {
			r = visit(pw, s, url, proxy, "")
		}
		results[s.ID] = r
	}
	// A returning visitor: Camoufox with a profile, a first visit and then a second session.
	name := "case77-" + runID + "-" + label
	camoufox := setups[len(setups)-1]
	visit(pw, camoufox, url, proxy, name)
	for i := 0; i < 60; i++ {
		free := false
		if list, ok := api("GET", "/v1/me/profiles")["profiles"].([]any); ok {
			for _, p := range list {
				if m := p.(map[string]any); m["name"] == name && m["in_use"] == false {
					free = true
				}
			}
		}
		if free {
			break
		}
		time.Sleep(2 * time.Second)
	}
	results["camoufox_returning"] = visit(pw, camoufox, url, proxy, name)
	api("DELETE", "/v1/me/profiles/"+name)
	return map[string]any{"site": label, "results": results}
}

func main() {
	urls := strings.Split(os.Getenv("LAB_SITES_KASADA_AWSWAF"), ",")
	sites := make([]map[string]any, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) { defer wg.Done(); sites[i] = site(string(rune('A'+i)), u) }(i, u)
	}
	wg.Wait()
	names := map[string]string{"camoufox_returning": "Camoufox, returning profile (2nd visit)"}
	for _, s := range setups {
		names[s.ID] = s.Name
	}
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "setups": names, "sites": sites}, "", "  ")
	fmt.Println(string(out))
}
