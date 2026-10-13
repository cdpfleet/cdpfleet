// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
// Go's CDP-native clients (chromedp, Rod) work on cdpUrl too; this program uses playwright-go's
// ConnectOverCDP plus raw CDP sessions, so it matches the other languages command for command.
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
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const url = "https://books.toscrape.com/"

// Bytes the page pulled over the network, from its own resource timing (fresh context: no cache).
const transfer = `() => {
  const all = [...performance.getEntriesByType('navigation'), ...performance.getEntriesByType('resource')];
  return { resources: all.length, transfer_kb: Math.round(all.reduce((n, e) => n + (e.transferSize || 0), 0) / 1024) };
}`

type loadRow struct {
	Label         string `json:"label"`
	Resources     int    `json:"resources"`
	TransferKB    int    `json:"transfer_kb"`
	ImagesBlocked int    `json:"images_blocked"`
	LoadMs        int64  `json:"load_ms"`
}

type targets struct {
	PagesBefore int `json:"pages_before"`
	PagesAfter  int `json:"pages_after"`
}

type output struct {
	Client          string         `json:"client"`
	ConnectMs       int64          `json:"connect_ms"`
	Product         string         `json:"product"`
	ProtocolVersion string         `json:"protocol_version"`
	Targets         targets        `json:"targets"`
	Loads           []loadRow      `json:"loads"`
	Metrics         map[string]any `json:"metrics"`
	SessionEndedS   *float64       `json:"session_ended_s"`
}

func launch() (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new", "cdp": true, "record": false})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chrome/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch: %s %s", res.Status, msg)
	}
	var s map[string]any
	return s, json.NewDecoder(res.Body).Decode(&s)
}

func isLive(sessionID string) bool {
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me/threads", nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return true
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return strings.Contains(string(b), sessionID)
}

// playwright-go hands back JSON numbers as int or float64 depending on the value.
func num(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// One page in a fresh browser context, optionally aborting image requests.
func load(browser playwright.Browser, blockImages bool) (playwright.BrowserContext, playwright.Page, loadRow, error) {
	context, err := browser.NewContext()
	if err != nil {
		return nil, nil, loadRow{}, err
	}
	page, err := context.NewPage()
	if err != nil {
		return context, nil, loadRow{}, err
	}
	blocked := 0
	if blockImages {
		if err := page.Route("**/*", func(route playwright.Route) {
			if route.Request().ResourceType() == "image" {
				blocked++
				route.Abort()
			} else {
				route.Continue()
			}
		}); err != nil {
			return context, page, loadRow{}, err
		}
	}
	t := time.Now()
	for attempt := 1; ; attempt++ { // the proxy can drop a tunnel; retry
		_, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)})
		if err == nil {
			break
		}
		if attempt == 3 {
			return context, page, loadRow{}, err
		}
	}
	loadMs := time.Since(t).Milliseconds()
	v, err := page.Evaluate(transfer)
	if err != nil {
		return context, page, loadRow{}, err
	}
	m := v.(map[string]any)
	label := "everything"
	if blockImages {
		label = "images blocked"
	}
	return context, page, loadRow{label, num(m["resources"]), num(m["transfer_kb"]), blocked, loadMs}, nil
}

func pageCount(cdp playwright.CDPSession) (int, error) {
	r, err := cdp.Send("Target.getTargets", nil)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range r.(map[string]any)["targetInfos"].([]any) {
		if t.(map[string]any)["type"] == "page" {
			n++
		}
	}
	return n, nil
}

func measure(s map[string]any, out *output) error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	t := time.Now()
	browser, err := pw.Chromium.ConnectOverCDP(s["cdpUrl"].(string), playwright.BrowserTypeConnectOverCDPOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return err
	}
	out.ConnectMs = time.Since(t).Milliseconds()
	defer browser.Close() // closes the connection, which ends the cdpfleet session

	// Raw CDP at the browser level: what's really on the other end.
	cdp, err := browser.NewBrowserCDPSession()
	if err != nil {
		return err
	}
	v, err := cdp.Send("Browser.getVersion", nil)
	if err != nil {
		return err
	}
	version := v.(map[string]any)
	out.Product, out.ProtocolVersion = version["product"].(string), version["protocolVersion"].(string)
	before, err := pageCount(cdp)
	if err != nil {
		return err
	}
	fullCtx, fullPage, fullRow, err := load(browser, false)
	if err != nil {
		return err
	}
	leanCtx, _, leanRow, err := load(browser, true)
	if err != nil {
		return err
	}
	after, err := pageCount(cdp)
	if err != nil {
		return err
	}
	out.Targets = targets{before, after}
	out.Loads = []loadRow{fullRow, leanRow}

	// Performance.getMetrics over a page-level CDP session (Puppeteer's page.metrics()).
	pageCdp, err := fullCtx.NewCDPSession(fullPage)
	if err != nil {
		return err
	}
	if _, err := pageCdp.Send("Performance.enable", nil); err != nil {
		return err
	}
	r, err := pageCdp.Send("Performance.getMetrics", nil)
	if err != nil {
		return err
	}
	m := map[string]float64{}
	for _, x := range r.(map[string]any)["metrics"].([]any) {
		e := x.(map[string]any)
		m[e["name"].(string)], _ = e["value"].(float64)
	}
	out.Metrics = map[string]any{"dom_nodes": int(m["Nodes"]), "js_heap_used_mb": math.Round(m["JSHeapUsedSize"]/1048576*10) / 10}
	fullCtx.Close()
	leanCtx.Close()
	return nil
}

func run() (output, error) {
	out := output{Client: "playwright-go 0.6000 ConnectOverCDP"}
	s, err := launch()
	if err != nil {
		return out, err
	}
	if err := measure(s, &out); err != nil {
		return out, err
	}
	id := s["sessionId"].(string)
	closed := time.Now()
	for isLive(id) && time.Since(closed) < 60*time.Second {
		time.Sleep(time.Second)
	}
	if !isLive(id) {
		ended := math.Round(time.Since(closed).Seconds()*10) / 10
		out.SessionEndedS = &ended
	}
	return out, nil
}

func main() {
	out, err := run()
	if err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
