// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, LAB_SITES (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
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
	"os"
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

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words.
const state = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\s+/g, ' ');
  const len = document.body ? document.body.innerText.length : 0;
  if (/sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) && len < 4000) return 'blocked';
  if (/just a moment|performing security verification|verify you are human|checking your browser|checking if the site connection is secure/i.test(t) && len < 4000) return 'challenge';
  return 'content';
})()`

type cell struct {
	Outcome string `json:"outcome"`
	Status  int    `json:"status"`
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

// One session, one load: the status of the page's own response, then its state 8 s later.
func visit(pw *playwright.Playwright, s setup, url, proxy, profile string) cell {
	ws, err := launch(s.Engine, s.Headless, proxy, profile)
	if err != nil {
		return cell{"error", 0}
	}
	bt := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}[s.Type]
	browser, err := bt.Connect(ws, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return cell{"error", 0}
	}
	defer browser.Close()
	var ctx playwright.BrowserContext
	if profile != "" {
		ctx = browser.Contexts()[0]
	} else if ctx, err = browser.NewContext(); err != nil {
		return cell{"error", 0}
	}
	tab, err := ctx.NewPage()
	if err != nil {
		return cell{"error", 0}
	}
	res, err := tab.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)})
	if err != nil {
		return cell{"error", 0}
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
	if outcome == "content" && status >= 400 {
		outcome = "blocked"
	}
	return cell{outcome, status}
}

// One sticky residential exit per site; its setups run one after another (one session at a time per
// site), and the sites run side by side. A network error gets one more try.
func site(label, url string) map[string]any {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	proxy := "cdpfleet-resi-country-us-session-c70" + runID + label + "-lifetime-30"
	results := map[string]cell{}
	for _, s := range setups {
		r := visit(pw, s, url, proxy, "")
		if r.Outcome == "error" {
			r = visit(pw, s, url, proxy, "")
		}
		results[s.ID] = r
	}
	// A returning visitor: Camoufox with a profile, a first visit and then a second session.
	name := "case70-" + runID + "-" + label
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
	urls := strings.Split(os.Getenv("LAB_SITES"), ",")
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
