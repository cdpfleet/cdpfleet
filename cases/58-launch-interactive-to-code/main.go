// go get github.com/playwright-community/playwright-go@v0.6000.0 github.com/gorilla/websocket@v1.5.3
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/playwright-community/playwright-go"
)

var (
	key   = os.Getenv("CDPFLEET_API_KEY")
	proxy = os.Getenv("PROXY_URL")
)

func call(method, u string, body any) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, u, r)
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	return res, data, err
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, opts map[string]any) map[string]any {
	for attempt := 1; ; attempt++ {
		res, data, err := call("POST", "https://starter.cdpfleet.com/"+engine+"/session", opts)
		if err != nil {
			log.Fatal(err)
		}
		if res.StatusCode == 200 {
			var out map[string]any
			json.Unmarshal(data, &out)
			return out
		}
		if res.StatusCode != 503 || attempt == 5 {
			log.Fatalf("launch %d %s", res.StatusCode, data)
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// The account API's session history: how long each session ran and why it ended.
func history(sessionID string) any {
	since := url.QueryEscape(time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))
	for i := 0; i < 30; i++ {
		_, data, err := call("GET", "https://cdpfleet.com/v1/me/sessions?limit=50&from="+since, nil)
		if err != nil {
			log.Fatal(err)
		}
		var list struct {
			Sessions []struct {
				SessionID string  `json:"session_id"`
				StartedAt string  `json:"started_at"`
				EndedAt   *string `json:"ended_at"`
				EndReason string  `json:"end_reason"`
			} `json:"sessions"`
		}
		json.Unmarshal(data, &list)
		for _, s := range list.Sessions {
			if s.SessionID == sessionID && s.EndedAt != nil {
				start, _ := time.Parse(time.RFC3339, s.StartedAt)
				end, _ := time.Parse(time.RFC3339, *s.EndedAt)
				return map[string]any{"seconds": int(math.Round(end.Sub(start).Seconds())), "end_reason": s.EndReason}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func main() {
	// 1. Launch with a start page and no client: the browser opens it by itself. A twin session
	//    with the same options gets no viewer at all, to compare with.
	body := map[string]any{"proxy": proxy, "headless": true, "live_view": true, "record": false,
		"start_url": "https://httpbin.org/", "inactivity_timeout": "30s"}
	session := launch("chromium", body)
	twin := launch("chromium", body)
	wsURL := session["wsUrl"].(string)

	// 2. A live link in control mode — what "Launch interactive" in the dashboard opens for you.
	u := must(url.Parse(wsURL))
	_, data, err := call("POST", "https://"+u.Host+"/admin/session/live", map[string]any{"session_id": session["sessionId"], "mode": "control"})
	if err != nil {
		log.Fatal(err)
	}
	var live struct {
		LiveURL  string `json:"live_url"`
		Protocol string `json:"protocol"`
	}
	json.Unmarshal(data, &live)

	// The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
	viewer, _, err := websocket.DefaultDialer.Dial(live.LiveURL, nil)
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	seen := []string{}
	frames := 0
	go func() {
		for {
			kind, msg, err := viewer.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			if kind == websocket.BinaryMessage {
				frames++
			} else {
				var m struct {
					Type    string `json:"type"`
					Current string `json:"current"`
					Pages   []struct {
						TargetID string `json:"targetId"`
						URL      string `json:"url"`
					} `json:"pages"`
				}
				if json.Unmarshal(msg, &m) == nil && m.Type == "pages" {
					for _, p := range m.Pages {
						if p.TargetID == m.Current && p.URL != "" && (len(seen) == 0 || seen[len(seen)-1] != p.URL) {
							seen = append(seen, p.URL)
						}
					}
				}
			}
			mu.Unlock()
		}
	}()
	send := func(m map[string]any) {
		if err := viewer.WriteJSON(m); err != nil {
			log.Fatal(err)
		}
	}
	has := func(part string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range seen {
			if strings.Contains(s, part) {
				return true
			}
		}
		return false
	}
	waitForURL := func(part string) bool {
		for i := 0; i < 60 && !has(part); i++ {
			time.Sleep(500 * time.Millisecond)
		}
		return has(part)
	}
	pressKey := func(k string) {
		send(map[string]any{"type": "key", "action": "keyDown", "key": k, "code": k})
		send(map[string]any{"type": "key", "action": "keyUp", "key": k, "code": k})
	}

	// 3. The person (here a scripted stand-in): goes to the form from the address bar, then fills
	//    the first three fields from the keyboard — Tab to a field, type.
	startShown := waitForURL("httpbin.org")
	send(map[string]any{"type": "navigate", "url": "https://httpbin.org/forms/post"})
	formShown := waitForURL("/forms/post")
	time.Sleep(1500 * time.Millisecond)
	for _, text := range []string{"Ada Lovelace", "+44 20 7946 0000", "ada@example.com"} {
		pressKey("Tab")
		time.Sleep(200 * time.Millisecond)
		send(map[string]any{"type": "text", "text": text})
		time.Sleep(300 * time.Millisecond)
	}
	// 4. Then only watches — no input for 45 s, past the 30 s inactivity_timeout.
	time.Sleep(45 * time.Second)

	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	headers := map[string]string{"x-api-key": key}

	// The unwatched twin has had no input and no viewer for as long: it should be gone.
	twinAlive := true
	if b, err := pw.Chromium.Connect(twin["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: headers, Timeout: playwright.Float(15000)}); err != nil {
		twinAlive = false
	} else {
		b.Close()
	}

	// 5. Your code joins the same browser and finds the person's page in the default context.
	browser := must(pw.Chromium.Connect(wsURL, playwright.BrowserTypeConnectOptions{Headers: headers}))
	mu.Lock()
	out := map[string]any{
		"live_protocol": live.Protocol, "start_page_shown": startShown, "form_shown": formShown, "frames_while_by_hand": frames,
		"watched_session_alive_after_45s": browser.IsConnected(), "unwatched_twin_alive_after_45s": twinAlive,
	}
	mu.Unlock()
	func() {
		defer browser.Close()
		defer viewer.Close()
		pages := browser.Contexts()[0].Pages()
		var page playwright.Page
		found := []string{}
		for _, p := range pages {
			found = append(found, p.URL())
			if strings.Contains(p.URL(), "/forms/post") {
				page = p
			}
		}
		out["pages_found"] = found
		typed := map[string]string{}
		for _, name := range []string{"custname", "custtel", "custemail"} {
			typed[name] = must(page.InputValue("input[name=" + name + "]"))
		}
		out["typed_by_hand"] = typed
		// 6. The code finishes the job: the choices a script makes reliably, then submit.
		must(0, page.Check("input[name=size][value=medium]"))
		must(0, page.Check("input[name=topping][value=cheese]"))
		must(0, page.Fill("input[name=delivery]", "19:30"))
		// No URL pattern: playwright-go v0.6000 can't match URLs in a context it didn't create (nil options).
		must(page.ExpectNavigation(func() error { return page.Click("form button") }))
		var echo map[string]any
		must(0, json.Unmarshal([]byte(must(page.TextContent("body"))), &echo))
		out["submitted"] = echo["form"]
		time.Sleep(1500 * time.Millisecond)
		mu.Lock()
		out["viewer_saw"] = append([]string{}, seen...)
		mu.Unlock()
	}()

	// 7. The account API's session history.
	out["watched_session"] = history(session["sessionId"].(string))
	out["unwatched_twin"] = history(twin["sessionId"].(string))
	printed, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(printed))
}
