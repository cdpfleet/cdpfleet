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
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const url = "https://books.toscrape.com/"

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

type live struct {
	Ms    int64 `json:"ms"`
	Books int   `json:"books"`
}

type replay struct {
	Ms            int64 `json:"ms"`
	Books         int   `json:"books"`
	ServedFromHar int64 `json:"served_from_har"`
	Refused       int64 `json:"refused"`
}

type output struct {
	HarEntries   int    `json:"har_entries"`
	HarKb        int64  `json:"har_kb"`
	Live         live   `json:"live"`
	Replay       replay `json:"replay"`
	PageNotInHar string `json:"page_not_in_har"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		return err
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return err
	}
	defer browser.Close()
	dir, err := os.MkdirTemp("", "har-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	har := filepath.Join(dir, "books.har")

	// 1. Record: every request of the page goes into a HAR file on this machine.
	rec, err := browser.NewContext(playwright.BrowserNewContextOptions{
		RecordHarPath: playwright.String(har),
		RecordHarMode: playwright.HarModeMinimal,
	})
	if err != nil {
		return err
	}
	p1, err := rec.NewPage()
	if err != nil {
		return err
	}
	t := time.Now()
	if _, err := p1.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)}); err != nil {
		return err
	}
	liveMs := time.Since(t).Milliseconds()
	liveBooks, err := p1.Locator("article.product_pod").Count()
	if err != nil {
		return err
	}
	if err := rec.Close(); err != nil { // writes the HAR
		return err
	}
	raw, err := os.ReadFile(har)
	if err != nil {
		return err
	}
	var harFile struct {
		Log struct {
			Entries []json.RawMessage `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(raw, &harFile); err != nil {
		return err
	}

	// 2. Replay: serve every request from the HAR; anything not in it is refused, so the
	//    page can't quietly reach the network.
	rep, err := browser.NewContext()
	if err != nil {
		return err
	}
	var fromHar, refused atomic.Int64
	rep.OnRequestFinished(func(playwright.Request) { fromHar.Add(1) })
	rep.OnRequestFailed(func(playwright.Request) { refused.Add(1) })
	if err := rep.RouteFromHAR(har, playwright.BrowserContextRouteFromHAROptions{NotFound: playwright.HarNotFoundAbort}); err != nil {
		return err
	}
	p2, err := rep.NewPage()
	if err != nil {
		return err
	}
	t = time.Now()
	if _, err := p2.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)}); err != nil {
		return err
	}
	replayMs := time.Since(t).Milliseconds()
	replayBooks, err := p2.Locator("article.product_pod").Count()
	if err != nil {
		return err
	}
	// A page the HAR doesn't have: refused instead of fetched.
	missing := "loaded"
	if _, err := p2.Goto("https://books.toscrape.com/catalogue/page-2.html"); err != nil {
		if regexp.MustCompile(`(?i)ERR_FAILED|aborted`).MatchString(err.Error()) {
			missing = "refused"
		} else {
			missing = strings.SplitN(err.Error(), "\n", 2)[0]
		}
	}
	if err := rep.Close(); err != nil {
		return err
	}

	out := output{
		HarEntries:   len(harFile.Log.Entries),
		HarKb:        int64(math.Round(float64(len(raw)) / 1024)),
		Live:         live{Ms: liveMs, Books: liveBooks},
		Replay:       replay{Ms: replayMs, Books: replayBooks, ServedFromHar: fromHar.Load(), Refused: refused.Load()},
		PageNotInHar: missing,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
	return nil
}
