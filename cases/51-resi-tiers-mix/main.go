// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (no proxy of your own needed: the cdpfleet residential proxy, billed per GB by tier)
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
	"slices"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

const pageURL = "https://commons.wikimedia.org/wiki/Category:Cats"

var (
	key   = os.Getenv("CDPFLEET_API_KEY")
	price = map[int]float64{1: 5, 4: 0.5}                              // USD per GB (10^9 bytes)
	cdn   = []string{"thumb.wikimedia.org", "upload.wikimedia.org"} // the images; HTML, CSS and JS come from commons.wikimedia.org
	// One session, two prices: the page through Tier 1, the image hosts through Tier 4.
	launch = map[string]any{
		"headless":    "new",
		"record":      false,
		"proxy":       "cdpfleet-resi-country-us",
		"proxy_rules": []map[string]any{{"hosts": cdn, "proxy": "cdpfleet-resi-tier-4-country-us"}},
	}
)

func round(x float64) int64 { return int64(math.Floor(x + 0.5)) }

// Today's residential traffic per tier, as billed.
func usedToday() (map[int]int64, error) {
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me/proxy-balance", nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var b struct {
		UsedToday []struct {
			Tier  int   `json:"tier"`
			Bytes int64 `json:"bytes"`
		} `json:"used_today"`
	}
	if err := json.NewDecoder(res.Body).Decode(&b); err != nil {
		return nil, err
	}
	used := map[int]int64{}
	for _, t := range b.UsedToday {
		used[t.Tier] = t.Bytes
	}
	return used, nil
}

type runOut struct {
	Requests      int   `json:"requests"`
	ImageRequests int   `json:"image_requests"`
	PageKB        int64 `json:"page_kb"`
	ImageKB       int64 `json:"image_kb"`
}

// Load the gallery, scroll to the end, and add up every request's bytes by host group.
func load(pw *playwright.Playwright) (runOut, error) {
	body, _ := json.Marshal(launch)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return runOut{}, err
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		return runOut{}, fmt.Errorf("launch %d %s", res.StatusCode, b)
	}
	var session struct {
		WsURL string `json:"wsUrl"`
	}
	if err := json.Unmarshal(b, &session); err != nil {
		return runOut{}, err
	}
	browser, err := pw.Chromium.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return runOut{}, err
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return runOut{}, err
	}
	var mu sync.Mutex
	var done []playwright.Request
	page.OnRequestFinished(func(r playwright.Request) { mu.Lock(); done = append(done, r); mu.Unlock() })
	// Residential peers drop the odd connection: retry the navigation once.
	for attempt := 1; ; attempt++ {
		_, err = page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad, Timeout: playwright.Float(60000)})
		if err == nil {
			break
		}
		if attempt == 2 {
			return runOut{}, err
		}
	}
	for i := 0; i < 20; i++ { // lazy-loaded thumbnails load as they scroll into view
		page.Mouse().Wheel(0, 1500)
		page.WaitForTimeout(250)
	}
	page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateNetworkidle, Timeout: playwright.Float(15000)})
	mu.Lock()
	finished := slices.Clone(done)
	mu.Unlock()
	var pageBytes, imageBytes int64
	images := 0
	for _, r := range finished {
		s, err := r.Sizes()
		if err != nil {
			continue
		}
		total := int64(s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize)
		u, _ := url.Parse(r.URL())
		if slices.Contains(cdn, u.Hostname()) {
			imageBytes += total
			images++
		} else {
			pageBytes += total
		}
	}
	return runOut{len(finished), images, round(float64(pageBytes) / 1000), round(float64(imageBytes) / 1000)}, nil
}

func run() error {
	before, err := usedToday()
	if err != nil {
		return err
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	r, err := load(pw)
	pw.Stop()
	if err != nil {
		return err
	}

	// Metering trails the traffic by about a minute: wait until Tier 4 shows the images.
	after := before
	for i := 0; i < 18 && float64(after[4]-before[4])/1000 < float64(r.ImageKB)*0.8; i++ {
		time.Sleep(10 * time.Second)
		if after, err = usedToday(); err != nil {
			return err
		}
	}

	// What 1,000 such loads cost: everything on Tier 1 versus the split.
	per1000 := func(pageTier, imageTier int) float64 {
		return float64(round((float64(r.PageKB)*price[pageTier]+float64(r.ImageKB)*price[imageTier])/1e6*1000*100)) / 100
	}
	out := struct {
		Page       string  `json:"page"`
		Run        runOut  `json:"run"`
		ImageShare float64 `json:"image_share"`
		Cost       struct {
			AllTier1 float64 `json:"all_tier_1"`
			Split    float64 `json:"page_tier_1_images_tier_4"`
		} `json:"cost_per_1000_loads_usd"`
		Billed struct {
			Tier1 int64 `json:"tier_1"`
			Tier4 int64 `json:"tier_4"`
		} `json:"billed_kb"`
	}{Page: pageURL, Run: r, ImageShare: float64(round(float64(r.ImageKB)/float64(r.ImageKB+r.PageKB)*100)) / 100}
	out.Cost.AllTier1, out.Cost.Split = per1000(1, 1), per1000(1, 4)
	out.Billed.Tier1, out.Billed.Tier4 = round(float64(after[1]-before[1])/1000), round(float64(after[4]-before[4])/1000)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
