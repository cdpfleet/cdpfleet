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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// Three visitors who must not see each other's state — in ONE browser session (1 thread).
var personas = []struct{ name, locale, timezone string }{
	{"alice", "en-US", "America/New_York"},
	{"bruno", "pt-BR", "America/Sao_Paulo"},
	{"chie", "ja-JP", "Asia/Tokyo"},
}

const seen = "() => ({\n" +
	"  cookie: document.cookie,\n" +
	"  storage_owner: localStorage.getItem('owner'),\n" +
	"  language: navigator.language,\n" +
	"  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,\n" +
	"  clock: new Date('2026-10-05T12:00:00Z').toLocaleTimeString(),\n" +
	"})"

type row struct {
	Persona      string `json:"persona"`
	Threads      int    `json:"threads"`
	Cookie       any    `json:"cookie"`
	StorageOwner any    `json:"storage_owner"`
	Language     any    `json:"language"`
	Timezone     any    `json:"timezone"`
	Clock        any    `json:"clock"`
	ExitIP       any    `json:"exit_ip"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
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

	pages := []playwright.Page{}
	for _, p := range personas {
		// Each context is a separate profile: its own cookies, storage, locale and clock.
		context := must(browser.NewContext(playwright.BrowserNewContextOptions{Locale: playwright.String(p.locale), TimezoneId: playwright.String(p.timezone)}))
		must(0, context.AddCookies([]playwright.OptionalCookie{{Name: "session", Value: p.name + "-token", Domain: playwright.String("example.com"), Path: playwright.String("/")}}))
		page := must(context.NewPage())
		must(page.Goto("https://example.com/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
		must(page.Evaluate("(n) => localStorage.setItem('owner', n)", p.name))
		pages = append(pages, page)
	}
	out := []row{}
	for i, p := range personas {
		page := pages[i]
		// Read everything after all three exist, so any leak between them would show.
		s := must(page.Evaluate(seen)).(map[string]any)
		ipRes := must(page.Goto("http://ip-api.com/json/?fields=query", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
		var ip map[string]any
		must(0, ipRes.JSON(&ip))
		out = append(out, row{p.name, int(session["weight"].(float64)), s["cookie"], s["storage_owner"], s["language"], s["timezone"], s["clock"], ip["query"]})
	}
	text, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(text))
}
