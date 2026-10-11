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
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const url = "https://books.toscrape.com/"

// Chrome DevTools' presets (throughput in bytes/s, latency in ms).
var profiles = []struct {
	name       string
	conditions map[string]any
}{
	{"no throttling", nil},
	{"Fast 4G", map[string]any{"offline": false, "latency": 150, "downloadThroughput": 9 * 1024 * 1024 / 8.0, "uploadThroughput": 1.5 * 1024 * 1024 / 8}},
	{"Slow 3G", map[string]any{"offline": false, "latency": 2000, "downloadThroughput": 400 * 1024 / 8.0, "uploadThroughput": 400 * 1024 / 8.0}},
}

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

type row struct {
	Profile string `json:"profile"`
	HTMLMs  int64  `json:"html_ms"`
	LoadMs  int64  `json:"load_ms"`
}

type output struct {
	Rows    []row  `json:"rows"`
	Offline string `json:"offline"`
}

// newPage opens a fresh context (no cache between runs) with a CDP session on its page.
// A CDP session works over the normal Playwright connection on Chromium-based browsers.
func newPage(browser playwright.Browser) (playwright.BrowserContext, playwright.Page, playwright.CDPSession) {
	context, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	page, err := context.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	cdp, err := context.NewCDPSession(page)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := cdp.Send("Network.enable", nil); err != nil {
		log.Fatal(err)
	}
	return context, page, cdp
}

func run() (output, error) {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		return output{}, err
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return output{}, err
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return output{}, err
	}
	defer browser.Close()

	out := output{Rows: []row{}}
	for _, p := range profiles {
		context, page, cdp := newPage(browser)
		if p.conditions != nil {
			if _, err := cdp.Send("Network.emulateNetworkConditions", p.conditions); err != nil {
				return out, err
			}
		}
		t := time.Now()
		if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(120000)}); err != nil {
			return out, err
		}
		htmlMs := time.Since(t).Milliseconds()
		if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad, Timeout: playwright.Float(180000)}); err != nil {
			return out, err
		}
		out.Rows = append(out.Rows, row{p.name, htmlMs, time.Since(t).Milliseconds()})
		context.Close()
	}

	// Offline: the page fails like it would on a dead connection.
	context, page, cdp := newPage(browser)
	if _, err := cdp.Send("Network.emulateNetworkConditions", map[string]any{"offline": true, "latency": 0, "downloadThroughput": -1, "uploadThroughput": -1}); err != nil {
		return out, err
	}
	if _, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(30000)}); err != nil {
		if m := regexp.MustCompile(`net::[A-Z_]+`).FindString(err.Error()); m != "" {
			out.Offline = m
		} else {
			out.Offline = strings.SplitN(err.Error(), "\n", 2)[0]
		}
	} else {
		out.Offline = "loaded"
	}
	context.Close()
	return out, nil
}

func main() {
	out, err := run()
	if err != nil {
		log.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
