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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const waitSeconds = 120 // between the two snapshots — in a real watcher, your scheduler's interval

// What to watch: a catalogue page (its price only changes when the shop changes it) and a live
// market price that moves by the second.
type target struct{ name, url, read string }

var targets = []target{
	{"Book price", "https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html",
		`() => ({ price: document.querySelector('.product_main .price_color').textContent.trim(), stock: document.querySelector('.product_main .availability').textContent.trim().replace(/\s+/g, ' ') })`},
	{"Bitcoin spot (USD)", "https://api.coinbase.com/v2/prices/BTC-USD/spot", "() => ({ price: JSON.parse(document.body.innerText).data.amount })"},
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session map[string]any
			return session, json.Unmarshal(b, &session)
		}
		if res.StatusCode != 503 || attempt == 5 {
			return nil, fmt.Errorf("launch %d %s", res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

type snap struct {
	at      string
	seconds float64
	values  map[string]map[string]string
}

// One snapshot = one short session: launch, read every target, close. Nothing is billed while
// waiting, and an idle session would end after 2 minutes (the default) anyway.
func snapshot(pw *playwright.Playwright) (snap, error) {
	t := time.Now()
	out := snap{values: map[string]map[string]string{}}
	s, err := launch(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true, "record": false})
	if err != nil {
		return out, err
	}
	browser, err := pw.Chromium.Connect(s["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return out, err
	}
	for _, tg := range targets {
		for attempt := 1; ; attempt++ { // proxies drop a tunnel now and then
			var raw any
			_, err = page.Goto(tg.url, playwright.PageGotoOptions{Timeout: playwright.Float(45000)})
			if err == nil {
				raw, err = page.Evaluate(tg.read)
			}
			if err == nil {
				vals := map[string]string{}
				for k, v := range raw.(map[string]any) {
					vals[k] = fmt.Sprint(v)
				}
				out.values[tg.name] = vals
				break
			}
			if attempt == 3 {
				return out, err
			}
		}
	}
	out.at = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	out.seconds = math.Round(time.Since(t).Seconds()*10) / 10
	return out, nil
}

var nonNumeric = regexp.MustCompile(`[^0-9.]`)

func number(v string) float64 {
	n, _ := strconv.ParseFloat(nonNumeric.ReplaceAllString(v, ""), 64)
	return n
}

type change struct {
	Field   string   `json:"field"`
	Before  string   `json:"before"`
	After   string   `json:"after"`
	Percent *float64 `json:"percent,omitempty"`
}

type targetDiff struct {
	Target  string            `json:"target"`
	Changed bool              `json:"changed"`
	Changes []change          `json:"changes"`
	Now     map[string]string `json:"now"`
}

// Field-by-field diff of two snapshots, with the percentage for numeric prices.
func diff(a, b snap) []targetDiff {
	out := []targetDiff{}
	for _, tg := range targets {
		changes := []change{}
		for k, x := range a.values[tg.name] {
			y := b.values[tg.name][k]
			if x == y {
				continue
			}
			c := change{Field: k, Before: x, After: y}
			if nx, ny := number(x), number(y); nx != 0 && ny != 0 {
				p := math.Round((ny-nx)/nx*1e4) / 100
				c.Percent = &p
			}
			changes = append(changes, c)
		}
		out = append(out, targetDiff{tg.name, len(changes) > 0, changes, b.values[tg.name]})
	}
	return out
}

func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	first, err := snapshot(pw)
	if err != nil {
		return err
	}
	time.Sleep(waitSeconds * time.Second)
	second, err := snapshot(pw)
	if err != nil {
		return err
	}
	// Field order matches the other languages' output.
	out := struct {
		WaitSeconds   int          `json:"wait_seconds"`
		BilledSeconds float64      `json:"billed_seconds"`
		FirstAt       string       `json:"first_at"`
		SecondAt      string       `json:"second_at"`
		Diff          []targetDiff `json:"diff"`
	}{waitSeconds, math.Round((first.seconds+second.seconds)*10) / 10, first.at, second.at, diff(first, second)}
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
