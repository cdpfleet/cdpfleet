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
	"strings"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4).
const peet = "https://tls.peet.ws/api/all"

type row struct {
	Path             string `json:"path"`
	RunsOn           string `json:"runs_on"`
	ExitIP           string `json:"exit_ip"`
	ExitType         string `json:"exit_type"`
	UserAgent        any    `json:"user_agent"`
	HTTPVersion      any    `json:"http_version"`
	JA4              string `json:"ja4"`
	TLSExtensions    int    `json:"tls_extensions"`
	ResumedTLS       bool   `json:"resumed_tls"`
	SameTLSAsBrowser bool   `json:"same_tls_stack_as_browser"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// GET a JSON document with Go's own HTTP client: no proxy, the default user agent.
func getJSON(url string) map[string]any {
	res := must(http.Get(url))
	defer res.Body.Close()
	var v map[string]any
	must(0, json.NewDecoder(res.Body).Decode(&v))
	return v
}

func makeRow(path string, seen map[string]any, runsOn string) row {
	ip := strings.Split(seen["ip"].(string), ":")[0]
	// Who owns the exit: a residential ISP (your proxy) or a hosting provider (a server)?
	who := getJSON("http://ip-api.com/json/" + ip + "?fields=hosting")
	exitType := "residential"
	if hosting, _ := who["hosting"].(bool); hosting {
		exitType = "datacenter"
	}
	tls := seen["tls"].(map[string]any)
	extensions, _ := tls["extensions"].([]any)
	resumed := false
	for _, e := range extensions {
		if name, _ := e.(map[string]any)["name"].(string); strings.Contains(name, "pre_shared_key") {
			resumed = true
		}
	}
	return row{Path: path, RunsOn: runsOn, ExitIP: ip, ExitType: exitType, UserAgent: seen["user_agent"], HTTPVersion: seen["http_version"],
		JA4: tls["ja4"].(string), TLSExtensions: len(extensions), ResumedTLS: resumed}
}

func main() {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		log.Fatalf("launch %s %s", res.Status, msg)
	}
	var session map[string]any
	must(0, json.NewDecoder(res.Body).Decode(&session))

	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()

	page := must(browser.NewPage())
	// 1. A navigation: the browser itself makes the request.
	var nav map[string]any
	must(0, must(page.Goto(peet, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})).JSON(&nav))
	// 2. fetch() inside the page (same origin): the browser's network stack, cookies and headers.
	inPage := must(page.Evaluate("() => fetch('/api/all').then((r) => r.json())")).(map[string]any)
	// 3. page.Request(): Playwright's own HTTP client, run by the Playwright server next to the browser.
	var viaRequest map[string]any
	must(0, must(page.Request().Get(peet, playwright.APIRequestContextGetOptions{Timeout: playwright.Float(60000)})).JSON(&viaRequest))
	// 4. Your own HTTP client on your machine, for reference.
	local := getJSON(peet)

	out := []row{
		makeRow("page.goto", nav, "the browser"),
		makeRow("fetch() in page.evaluate", inPage, "the browser"),
		makeRow("page.request.get", viaRequest, "Playwright server"),
		makeRow("fetch() in your script", local, "your machine"),
	}
	// JA4's middle part hashes the cipher suites: the same TLS stack keeps it across connections.
	browserCiphers := strings.Split(out[0].JA4, "_")[1]
	for i := range out {
		out[i].SameTLSAsBrowser = strings.Split(out[i].JA4, "_")[1] == browserCiphers
	}
	text, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(text))
}
