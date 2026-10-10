// go get github.com/playwright-community/playwright-go@v0.6000.0
//
// One-time driver setup (needs Node.js 18+): playwright-go can no longer download the
// 1.60.0 driver itself, so build it from npm and point PLAYWRIGHT_DRIVER_PATH at it:
//   mkdir -p ~/.cache/pw-driver-1.60.0 && cd ~/.cache/pw-driver-1.60.0 &&
//     curl -sL https://registry.npmjs.org/playwright-core/-/playwright-core-1.60.0.tgz | tar xz &&
//     ln -sf "$(command -v node)" node
//   export PLAYWRIGHT_DRIVER_PATH=~/.cache/pw-driver-1.60.0
// Browsers run on cdpfleet, so no browsers are downloaded (SkipInstallBrowsers).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/playwright-community/playwright-go"
)

func main() {
	key := os.Getenv("CDPFLEET_API_KEY")

	// 1. Launch the browser
	body := `{
	  "proxy": "http://user:pass@proxy.example.com:8080",
	  "headless": "new"
	}`
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/yandex/session", strings.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		log.Fatalf("launch failed: %s %s", res.Status, msg)
	}
	var session struct {
		WsURL string `json:"wsUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		log.Fatal(err)
	}

	// 2. Connect and drive it (Install fetches only the Playwright driver, once)
	opts := &playwright.RunOptions{SkipInstallBrowsers: true}
	if err := playwright.Install(opts); err != nil {
		log.Fatal(err)
	}
	pw, err := playwright.Run(opts)
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{
		Headers: map[string]string{"x-api-key": key},
	})
	if err != nil {
		log.Fatal(err)
	}
	page, err := browser.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := page.Goto("https://example.com"); err != nil {
		log.Fatal(err)
	}
	title, _ := page.Title()
	fmt.Println(title)
	browser.Close() // ends the session and stops billing
}
