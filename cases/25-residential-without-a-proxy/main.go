// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (no proxy of your own needed)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

type row struct {
	Label            string `json:"label"`
	Proxy            string `json:"proxy,omitempty"`
	Countries        string `json:"countries,omitempty"`
	DistinctIPs      int    `json:"distinct_ips,omitempty"`
	SameIPAsBrowser1 *bool  `json:"same_ip_as_browser_1"`
	Error            string `json:"error,omitempty"`
	firstIP          string
}

type exit struct {
	Query       string `json:"query"`
	CountryCode string `json:"countryCode"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// Residential peers drop a few percent of connections: retry a lookup up to 3 times.
func lookup(page playwright.Page, i int) exit {
	for attempt := 1; ; attempt++ {
		r, err := page.Goto(fmt.Sprintf("http://ip-api.com/json/?fields=query,countryCode&i=%d-%d", i, attempt),
			playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		var e exit
		if err == nil {
			if body, berr := r.Body(); berr == nil {
				if jerr := json.Unmarshal(body, &e); jerr == nil {
					return e
				} else {
					err = jerr
				}
			} else {
				err = berr
			}
		}
		if attempt == 3 {
			log.Fatal(err)
		}
	}
}

func run(pw *playwright.Playwright, label, proxy, session string) row {
	body, _ := json.Marshal(map[string]any{"proxy": proxy, "headless": "new"})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return row{Label: label, Error: fmt.Sprintf("launch %d %s", res.StatusCode, raw)}
	}
	var s struct {
		WsURL string `json:"wsUrl"`
	}
	must(0, json.Unmarshal(raw, &s))
	browser := must(pw.Chromium.Connect(s.WsURL, playwright.BrowserTypeConnectOptions{
		Headers: map[string]string{"x-api-key": key},
	}))
	defer browser.Close()
	page := must(browser.NewPage())
	// Three lookups on separate connections: rotating exits change, sticky ones don't.
	var exits []exit
	for i := 0; i < 3; i++ {
		exits = append(exits, lookup(page, i))
	}
	var countries []string
	seenCountry := map[string]bool{}
	ips := map[string]bool{}
	for _, e := range exits {
		if !seenCountry[e.CountryCode] {
			seenCountry[e.CountryCode] = true
			countries = append(countries, e.CountryCode)
		}
		ips[e.Query] = true
	}
	return row{
		Label:       label,
		Proxy:       strings.ReplaceAll(proxy, session, "<name>"),
		Countries:   strings.Join(countries, ","),
		DistinctIPs: len(ips),
		firstIP:     exits[0].Query,
	}
}

func main() {
	b36 := strconv.FormatInt(time.Now().UnixMilli(), 36)
	session := "c25" + b36[len(b36)-6:] // a sticky name, 1–10 of [A-Za-z0-9_]
	// Four launches, all on the cdpfleet residential proxy — the token is the whole proxy config.
	runs := [][2]string{
		{"rotating, any country", "cdpfleet-resi"},
		{"rotating, Germany", "cdpfleet-resi-country-de"},
		{"sticky US, browser 1", "cdpfleet-resi-country-us-session-" + session + "-lifetime-10"},
		{"sticky US, browser 2", "cdpfleet-resi-country-us-session-" + session + "-lifetime-10"},
	}

	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	var out []row
	for _, r := range runs { // in order, so browser 2 starts after browser 1 ended
		out = append(out, run(pw, r[0], r[1], session))
	}
	b1 := out[2]
	for i := range out {
		if strings.HasPrefix(out[i].Label, "sticky") {
			same := out[i].firstIP == b1.firstIP
			out[i].SameIPAsBrowser1 = &same
		}
	}
	// The balance the traffic is billed to (metered about once a minute, so it trails a little).
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me/proxy-balance", nil)
	req.Header.Set("x-api-key", key)
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	var bal struct {
		Allowed       bool    `json:"allowed"`
		PricePerGbUsd float64 `json:"price_per_gb_usd"`
	}
	must(0, json.NewDecoder(res.Body).Decode(&bal))

	doc := struct {
		Runs                       []row   `json:"runs"`
		StickyIPKeptAcrossBrowsers *bool   `json:"sticky_ip_kept_across_browsers"`
		BalanceAllowed             bool    `json:"balance_allowed"`
		PricePerGbUsd              float64 `json:"price_per_gb_usd"`
	}{out, out[3].SameIPAsBrowser1, bal.Allowed, bal.PricePerGbUsd}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	must(0, enc.Encode(doc))
}
