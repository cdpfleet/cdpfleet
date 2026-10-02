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
	"path/filepath"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// Keep it somewhere safe: it holds the login.
var stateFile = filepath.Join(os.TempDir(), "cdpfleet-state.json")

func launch(name string, options map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(options)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+name+"/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch %s: %s %s", name, res.Status, msg)
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func cookiesSeen(page playwright.Page) any {
	res := must(page.Goto("https://httpbin.org/cookies", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
	var v map[string]any
	must(0, res.JSON(&v))
	return v["cookies"]
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	headers := playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}
	opts := map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true}

	// Session 1 (Chromium): "log in", then save cookies + localStorage to a local file.
	s1 := must(launch("chromium", opts))
	b1 := must(pw.Chromium.Connect(s1["wsUrl"].(string), headers))
	ctx := must(b1.NewContext())
	page := must(ctx.NewPage())
	must(page.Goto("https://httpbin.org/cookies/set?session=abc123&user=alice", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
	must(page.Evaluate("localStorage.setItem('draft', 'half-written review')"))
	must(ctx.StorageState(playwright.BrowserContextStorageStateOptions{Path: playwright.String(stateFile)}))
	b1.Close() // the browser is gone; only the state file remains

	var saved struct {
		Cookies []struct{ Name, Domain string } `json:"cookies"`
		Origins []struct{ Origin string }       `json:"origins"`
	}
	raw := must(os.ReadFile(stateFile))
	must(0, json.Unmarshal(raw, &saved))
	savedCookies, origins := []string{}, []string{}
	for _, c := range saved.Cookies {
		savedCookies = append(savedCookies, c.Name+"@"+c.Domain)
	}
	for _, o := range saved.Origins {
		origins = append(origins, o.Origin)
	}

	// Session 2 (Firefox, a fresh browser on whichever server the fleet picks): restore it.
	s2 := must(launch("firefox", opts))
	b2 := must(pw.Firefox.Connect(s2["wsUrl"].(string), headers))
	defer b2.Close()
	restored := must(b2.NewContext(playwright.BrowserNewContextOptions{StorageStatePath: playwright.String(stateFile)}))
	page2 := must(restored.NewPage())
	cookies := cookiesSeen(page2)
	draft := must(page2.Evaluate("localStorage.getItem('draft')"))
	blank := must(must(b2.NewContext()).NewPage()) // the same browser without the state

	out, _ := json.MarshalIndent(map[string]any{
		"session_1":               map[string]any{"id": s1["sessionId"], "browser": "chromium", "saved_cookies": savedCookies, "saved_origins": origins},
		"state_file_bytes":        len(raw),
		"session_2":               map[string]any{"id": s2["sessionId"], "browser": "firefox", "cookies_sent": cookies, "local_storage_draft": draft},
		"session_2_without_state": map[string]any{"cookies_sent": cookiesSeen(blank)},
	}, "", "  ")
	fmt.Println(string(out))
}
