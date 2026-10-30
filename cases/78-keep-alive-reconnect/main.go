// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_DEDICATED_API_KEY (keep_alive needs dedicated threads), PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const starter = "https://starter.cdpfleet.com"
const api = "https://cdpfleet.com"
const idleS = 30 // inactivity_timeout: how long the session waits for a client
const site = "https://books.toscrape.com"

var key = os.Getenv("CDPFLEET_DEDICATED_API_KEY")
var proxy = os.Getenv("PROXY_URL")
var headers = map[string]string{"x-api-key": key}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func getJSON(path string, out any) {
	req, _ := http.NewRequest("GET", api+path, nil)
	req.Header.Set("x-api-key", key)
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	check(json.NewDecoder(res.Body).Decode(out))
}

type thread struct {
	SessionID      string `json:"session_id"`
	RunningSeconds *int   `json:"running_seconds"`
}

// The session as the account API sees it while it runs: listed in /v1/me/threads or not.
func listed(sid string) *thread {
	var r struct {
		Threads []thread `json:"threads"`
	}
	getJSON("/v1/me/threads", &r)
	for _, t := range r.Threads {
		if t.SessionID == sid {
			return &t
		}
	}
	return nil
}

// Today's thread-seconds on the account, closed and still running.
func usage() (closed, live int) {
	var u struct {
		Closed int `json:"closed_thread_seconds"`
		Live   int `json:"live_thread_seconds"`
	}
	getJSON("/v1/me/usage", &u)
	return u.Closed, u.Live
}

// A shared proxy drops the odd connection: one retry.
func gotoURL(page playwright.Page, url string) {
	if _, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
		must(page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}))
	}
}

func secs(a, b time.Time) float64 { return math.Round(b.Sub(a).Seconds()*10) / 10 }

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	closedBefore, _ := usage()

	// 1. Launch with keep_alive: a disconnect no longer ends the session.
	t0 := time.Now()
	body, _ := json.Marshal(map[string]any{"proxy": proxy, "headless": "new", "keep_alive": true, "inactivity_timeout": fmt.Sprintf("%ds", idleS)})
	req, _ := http.NewRequest("POST", starter+"/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	var session struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
	}
	check(json.NewDecoder(res.Body).Decode(&session))
	res.Body.Close()
	if res.StatusCode != 200 {
		log.Fatalf("launch failed: %d", res.StatusCode)
	}
	sid, wsURL := session.SessionID, session.WsURL
	opts := playwright.BrowserTypeConnectOptions{Headers: headers}

	// 2. First client: an agent works through the catalogue in one context, a second context on the side.
	browser := must(pw.Chromium.Connect(wsURL, opts))
	work := must(browser.NewContext())
	page := must(work.NewPage())
	gotoURL(page, site+"/catalogue/page-1.html")
	check(page.Locator("li.next a").Click()) // step 2 of the run: page 2
	check(work.AddCookies([]playwright.OptionalCookie{{Name: "agent_step", Value: "2", Domain: playwright.String("books.toscrape.com"), Path: playwright.String("/")}}))
	must(page.Evaluate("() => localStorage.setItem('agent_state', JSON.stringify({ seen: 40, next: 'page-3' }))"))
	gotoURL(must(work.NewPage()), site+"/catalogue/a-light-in-the-attic_1000/index.html")
	side := must(browser.NewContext())
	gotoURL(must(side.NewPage()), site+"/catalogue/category/books/poetry_23/index.html")
	pagesBefore := 0
	for _, c := range browser.Contexts() {
		pagesBefore += len(c.Pages())
	}
	before := map[string]any{"contexts": len(browser.Contexts()), "pages": pagesBefore, "url": page.URL()}

	// 3. The client goes away. With keep_alive, Close() only disconnects.
	check(browser.Close())
	tDetach := time.Now()
	time.Sleep(3 * time.Second)
	whileDetached := listed(sid)
	time.Sleep(12 * time.Second)

	// 4. A new client reconnects to the same wsUrl and carries on where the first one stopped.
	tReconnect := time.Now()
	browser = must(pw.Chromium.Connect(wsURL, opts))
	reconnectMs := time.Since(tReconnect).Milliseconds()
	detachedS := secs(tDetach, tReconnect)
	contexts := browser.Contexts()
	var urls []string
	var backCtx playwright.BrowserContext
	var back playwright.Page
	pages := 0
	for _, c := range contexts {
		for _, p := range c.Pages() {
			pages++
			urls = append(urls, strings.Replace(p.URL(), site, "", 1))
			if strings.Contains(p.URL(), "page-2.html") {
				backCtx, back = c, p
			}
		}
	}
	sort.Strings(urls)
	var cookie, local, continued any
	if back != nil {
		for _, c := range must(backCtx.Cookies(site)) {
			if c.Name == "agent_step" {
				cookie = c.Value
			}
		}
		local = must(back.Evaluate("() => localStorage.getItem('agent_state')"))
		check(back.Locator("li.next a").Click())
		// playwright-go 0.6000's WaitForURL panics on a context this client didn't create (it reads the
		// context's launch options, which a reconnected client doesn't have), so poll the URL instead.
		for i := 0; i < 150 && !strings.HasSuffix(back.URL(), "page-3.html"); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		continued = strings.Replace(back.URL(), site, "", 1)
	}

	// 5. The client leaves for good: the session waits inactivity_timeout, then ends.
	check(browser.Close())
	tLeave := time.Now()
	var goneAfterS, liveWaiting any
	for time.Since(tLeave) < (idleS+90)*time.Second {
		time.Sleep(2 * time.Second)
		if liveWaiting == nil && time.Since(tLeave) >= 20*time.Second {
			_, live := usage()
			liveWaiting = live
		}
		if listed(sid) == nil {
			goneAfterS = secs(tLeave, time.Now())
			break
		}
	}
	late := map[string]any{}
	if b, err := pw.Chromium.Connect(wsURL, playwright.BrowserTypeConnectOptions{Headers: headers, Timeout: playwright.Float(20000)}); err != nil {
		msg := strings.Split(err.Error(), "\n")[0]
		if len(msg) > 160 {
			msg = msg[:160]
		}
		late = map[string]any{"connected": false, "error": msg}
	} else {
		late = map[string]any{"connected": true, "contexts": len(b.Contexts())}
		b.Close()
	}

	// 6. What was billed: the session's record in the account history.
	time.Sleep(5 * time.Second)
	var hist struct {
		Sessions []map[string]any `json:"sessions"`
	}
	getJSON("/v1/me/sessions?limit=20", &hist)
	s := map[string]any{}
	for _, x := range hist.Sessions {
		if x["session_id"] == sid {
			s = x
		}
	}
	closedAfter, _ := usage()
	var running any
	if whileDetached != nil {
		running = whileDetached.RunningSeconds
	}

	out := map[string]any{
		"language":             "go",
		"inactivity_timeout_s": idleS,
		"before":               before,
		"detached":             map[string]any{"listed": whileDetached != nil, "running_seconds": running, "seconds": detachedS},
		"reconnect": map[string]any{"ms": reconnectMs, "contexts": len(contexts), "pages": pages, "urls": urls,
			"cookie": cookie, "local_storage": local, "continued": continued},
		"end": map[string]any{"gone_after_s": goneAfterS, "late_reconnect": late, "end_reason": s["end_reason"],
			"duration_seconds": s["duration_seconds"], "weight": s["weight"], "client_seconds": secs(t0, tLeave)},
		"usage":       map[string]any{"live_while_waiting": liveWaiting, "billed": closedAfter - closedBefore},
		"measured_at": time.Now().UTC().Format("2006-01-02"),
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
