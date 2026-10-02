// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

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

func pageJSON(page playwright.Page, u string) map[string]any {
	res := must(page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
	var v map[string]any
	if err := res.JSON(&v); err != nil {
		log.Fatal(err)
	}
	return v
}

func exitIP(page playwright.Page, host string) string {
	return pageJSON(page, "https://"+host+"/?format=json")["ip"].(string)
}

func cookies(page playwright.Page) any {
	return pageJSON(page, "https://httpbin.org/cookies")["cookies"]
}

func main() {
	session := must(launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "proxy_updatable": true, "headless": true}))
	wsURL := session["wsUrl"].(string)

	// Swap the proxy on the session's router (the host in wsUrl). Takes effect for new
	// connections; the browser, its tabs, cookies and storage stay as they are.
	swapProxy := func(proxy string) {
		body, _ := json.Marshal(map[string]any{"session_id": session["sessionId"], "proxy": proxy})
		req, _ := http.NewRequest("POST", "https://"+must(url.Parse(wsURL)).Host+"/admin/session/proxy", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res := must(http.DefaultClient.Do(req))
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			msg, _ := io.ReadAll(res.Body)
			log.Fatalf("swap: %s %s", res.Status, msg)
		}
	}

	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(wsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()

	context := must(browser.NewContext())
	page := must(context.NewPage())
	must(page.Goto("https://httpbin.org/cookies/set?cart=3-items&login=alice", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
	must(page.Evaluate("localStorage.setItem('draft', 'half-written review')"))
	before := map[string]any{"exit_ip": exitIP(page, "api.ipify.org"), "cookies": cookies(page)}

	t := time.Now()
	swapProxy(strings.Split(os.Getenv("SOCKS_PROXIES"), ",")[0])
	swapMs := time.Since(t).Milliseconds()

	// 1. Same tab, same host: the open keep-alive connection still goes through the old proxy.
	reusedConnection := exitIP(page, "api.ipify.org")
	// 2. Same tab, a host we haven't connected to yet: a new connection, so the new proxy.
	newConnection := exitIP(page, "api64.ipify.org")
	// 3. Move everything over: a new context (its own connection pool) with the old
	//    cookies and localStorage.
	state := must(context.StorageState())
	moved := must(browser.NewContext(playwright.BrowserNewContextOptions{StorageState: state.ToOptionalStorageState()}))
	context.Close()
	page2 := must(moved.NewPage())
	after := map[string]any{
		"exit_ip":       exitIP(page2, "api.ipify.org"),
		"cookies":       cookies(page2),
		"local_storage": must(page2.Evaluate("localStorage.getItem('draft')")),
	}
	out, _ := json.MarshalIndent(map[string]any{
		"before":                         before,
		"swap_ms":                        swapMs,
		"same_tab_reused_connection":     reusedConnection,
		"same_tab_new_connection":        newConnection,
		"new_context_with_storage_state": after,
		"same_browser_session":           browser.IsConnected(),
	}, "", "  ")
	fmt.Println(string(out))
}
