// go get github.com/playwright-community/playwright-go@v0.6000.0 github.com/cdpfleet/cdpfleet/sdk/go@v0.1.1
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL, COMMUNITY_API_KEY (a Community account: 2 threads, to reach the cap)
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	cdpfleet "github.com/cdpfleet/cdpfleet/sdk/go"
	"github.com/playwright-community/playwright-go"
)

const starter = "https://starter.cdpfleet.com"
const api = "https://cdpfleet.com"

var key = os.Getenv("CDPFLEET_API_KEY")
var freeKey = os.Getenv("COMMUNITY_API_KEY")
var proxy = os.Getenv("PROXY_URL")
var urls = []string{
	"https://books.toscrape.com/catalogue/page-1.html",
	"https://books.toscrape.com/catalogue/page-2.html",
	"https://books.toscrape.com/catalogue/page-3.html",
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

type result struct {
	Books       int     `json:"books"`
	FirstTitle  string  `json:"first_title"`
	ConnectedMs int64   `json:"connected_ms"`
	Seconds     float64 `json:"seconds"`
}

// The scrape every variant runs: three catalogue pages, every book title.
func scrape(browser playwright.Browser, start time.Time, connected time.Duration) result {
	page := must(browser.NewPage())
	var titles []string
	for _, u := range urls {
		if _, err := page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
			// A shared proxy drops the odd connection: one retry.
			must(page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}))
		}
		got := must(page.Locator("article.product_pod h3 a").EvaluateAll("as => as.map(a => a.getAttribute('title'))"))
		for _, t := range got.([]any) {
			titles = append(titles, t.(string))
		}
	}
	return result{len(titles), titles[0], connected.Milliseconds(), time.Since(start).Seconds()}
}

// ---- 1a. Plain Playwright: launch over HTTP yourself, then connect with the key header.
type launchReply struct {
	status     int
	retryAfter string
	body       map[string]any
}

func plainLaunch(k string, body map[string]any) launchReply {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", starter+"/chromium/session", bytes.NewReader(b))
	req.Header.Set("x-api-key", k)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	var data map[string]any
	_ = json.NewDecoder(res.Body).Decode(&data)
	return launchReply{res.StatusCode, res.Header.Get("retry-after"), data}
}

func plain(pw *playwright.Playwright) result {
	t := time.Now()
	r := plainLaunch(key, map[string]any{"proxy": proxy, "headless": true, "record": false})
	if r.status != 200 {
		log.Fatalf("launch %d %v", r.status, r.body)
	}
	browser := must(pw.Chromium.Connect(r.body["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close() // closing the connection ends the session
	return scrape(browser, t, time.Since(t))
}

// ---- 1b. The SDK: Launch() (retries 429/503 as the API asks), Connect(), Close().
func withSDK(pw *playwright.Playwright, fleet *cdpfleet.Client) result {
	t := time.Now()
	session := must(fleet.Launch("chromium", cdpfleet.Options{"proxy": proxy, "headless": true, "record": false}))
	defer session.Close()
	browser := must(session.Connect(pw.Chromium))
	return scrape(browser, t, time.Since(t))
}

// ---- 1c. The SDK's connect URL: one Connect() launches the browser (no HTTP call of your own).
func withConnectURL(pw *playwright.Playwright, fleet *cdpfleet.Client) result {
	t := time.Now()
	u := fleet.ConnectURL("chromium", cdpfleet.Options{"proxy": proxy, "headless": true, "record": false})
	browser := must(pw.Chromium.Connect(u, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": fleet.APIKey}}))
	defer browser.Close()
	return scrape(browser, t, time.Since(t))
}

// ---- 2. At the thread cap (Community: 2 threads). Hold both, then ask for a third.
func endSession(k, id string) {
	req, _ := http.NewRequest("DELETE", api+"/v1/me/threads/"+id, nil)
	req.Header.Set("x-api-key", k)
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
}

func liveThreads(k string) int {
	req, _ := http.NewRequest("GET", api+"/v1/me/threads", nil)
	req.Header.Set("x-api-key", k)
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	var data struct {
		Threads []any `json:"threads"`
	}
	_ = json.NewDecoder(res.Body).Decode(&data)
	return len(data.Threads)
}

func atTheCap() map[string]any {
	var held []string
	for i := 0; i < 2; i++ {
		r := plainLaunch(freeKey, map[string]any{"headless": true, "record": false})
		if r.status != 200 {
			log.Fatalf("hold %d %v", r.status, r.body)
		}
		held = append(held, r.body["sessionId"].(string))
		time.Sleep(1500 * time.Millisecond) // Community launches: at most 2 a second, 12 a minute
	}
	// Plain: the answer comes back at once and the retry is yours to write.
	third := plainLaunch(freeKey, map[string]any{"headless": true, "record": false})
	time.Sleep(1500 * time.Millisecond)
	// SDK: the same launch waits; free a thread 5 s later and see when it returns.
	freeFleet := must(cdpfleet.New(cdpfleet.WithAPIKey(freeKey)))
	first := held[0]
	held = held[1:]
	time.AfterFunc(5*time.Second, func() { endSession(freeKey, first) })
	t := time.Now()
	retry := map[string]any{"by": "sdk"}
	if s, err := freeFleet.Launch("chromium", cdpfleet.Options{"headless": true, "record": false}); err == nil {
		retry["got_session"], retry["waited_s"] = true, time.Since(t).Seconds()
		_ = s.End() // never connected: end it explicitly
	} else {
		var e *cdpfleet.Error
		retry["got_session"], retry["waited_s"] = false, time.Since(t).Seconds()
		if errors.As(err, &e) {
			retry["error"] = e.Code
		}
	}
	for _, id := range held {
		endSession(freeKey, id)
	}
	time.Sleep(3 * time.Second)
	var upgradeURL any
	if u, ok := third.body["upgrade"].(map[string]any); ok {
		upgradeURL = u["url"]
	}
	return map[string]any{
		"threads": 2,
		"plain": map[string]any{"status": third.status, "error": third.body["error"], "retry_after": third.retryAfter,
			"in_use": third.body["in_use"], "upgrade_url": upgradeURL},
		"retry":        retry,
		"threads_left": liveThreads(freeKey),
	}
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	fleet := must(cdpfleet.New()) // reads CDPFLEET_API_KEY
	out := map[string]any{"language": "go", "sdk": "cdpfleet 0.1.1 (Go module)"}
	out["scrape"] = map[string]any{"plain": plain(pw), "sdk": withSDK(pw, fleet), "connect_url": withConnectURL(pw, fleet)}
	out["cap"] = atTheCap()
	out["measured_at"] = time.Now().Format("2006-01-02")
	fmt.Println(string(must(json.MarshalIndent(out, "", "  "))))
}
