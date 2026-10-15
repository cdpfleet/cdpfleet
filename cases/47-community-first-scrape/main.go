// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: COMMUNITY_API_KEY
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("COMMUNITY_API_KEY")

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

type exit struct{ IP, Country string }

// The exit the site sees. Shared proxies drop the odd connection: retry a few times.
func exitOf(page playwright.Page) (exit, error) {
	for i := 1; ; i++ {
		_, err := page.Goto("http://ip-api.com/json", playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
		if err == nil {
			var text string
			if text, err = page.TextContent("body"); err == nil {
				var j struct {
					Query       string `json:"query"`
					CountryCode string `json:"countryCode"`
				}
				if err = json.Unmarshal([]byte(text), &j); err == nil {
					return exit{j.Query, j.CountryCode}, nil
				}
			}
		}
		if i == 4 {
			return exit{}, err
		}
		page.WaitForTimeout(float64(2000 * i))
	}
}

// One session: launch, connect, run work on a new page, close (ends the session and the clock).
func session(pw *playwright.Playwright, extra map[string]any, work func(playwright.Page) error) (int, error) {
	opts := map[string]any{"headless": true, "record": false}
	for k, v := range extra {
		opts[k] = v
	}
	s, err := launch(opts)
	if err != nil {
		return 0, err
	}
	browser, err := pw.Chromium.Connect(s["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return 0, err
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return 0, err
	}
	return int(s["weight"].(float64)), work(page)
}

type book struct {
	Title string `json:"title"`
	Price string `json:"price"`
}

func run() error {
	// 1. What the free plan includes.
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me", nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	var me struct {
		Subscription struct {
			PlanID   string `json:"plan_id"`
			Threads  int    `json:"threads"`
			Minutes  int    `json:"daily_minutes_per_thread"`
			MaxSess  int    `json:"max_session_seconds"`
			Features struct {
				MaxPages int `json:"max_pages"`
				MemoryMB int `json:"memory_mb"`
			} `json:"features"`
			PlanProxy *struct {
				Options map[string]bool `json:"options"`
			} `json:"plan_proxy"`
		} `json:"subscription"`
	}
	err = json.NewDecoder(res.Body).Decode(&me)
	res.Body.Close()
	if err != nil {
		return err
	}
	sub := me.Subscription
	var options map[string]bool
	if sub.PlanProxy != nil {
		options = sub.PlanProxy.Options
	}

	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()

	// 2. No proxy in the launch: the plan's included proxy is used. Scrape a catalogue page.
	var first exit
	var books []book
	weight, err := session(pw, map[string]any{}, func(page playwright.Page) error {
		var err error
		if first, err = exitOf(page); err != nil {
			return err
		}
		if _, err = page.Goto("https://books.toscrape.com/", playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
			return err
		}
		raw, err := page.EvalOnSelectorAll("article.product_pod", "els => els.map(e => ({ title: e.querySelector('h3 a').getAttribute('title'), price: e.querySelector('.price_color').textContent.trim() }))")
		if err != nil {
			return err
		}
		b, _ := json.Marshal(raw)
		return json.Unmarshal(b, &books)
	})
	if err != nil {
		return err
	}

	// 3. A cdpfleet-plan token picks a country and keeps the same exit across sessions.
	rnd := make([]byte, 3)
	rand.Read(rnd)
	token := "cdpfleet-plan-country-de-session-c47" + hex.EncodeToString(rnd)[:5] + "-lifetime-10"
	var a, b exit
	if _, err = session(pw, map[string]any{"proxy": token}, func(p playwright.Page) (err error) { a, err = exitOf(p); return }); err != nil {
		return err
	}
	if _, err = session(pw, map[string]any{"proxy": token}, func(p playwright.Page) (err error) { b, err = exitOf(p); return }); err != nil {
		return err
	}

	// Field order matches the other languages' output.
	type planOut struct {
		Plan         string          `json:"plan"`
		Threads      int             `json:"threads"`
		Minutes      int             `json:"minutes_per_thread_per_day"`
		MaxSessMin   int             `json:"max_session_minutes"`
		MaxPages     int             `json:"max_pages"`
		MemoryMB     int             `json:"memory_mb"`
		ProxyOptions map[string]bool `json:"proxy_options"`
	}
	out := struct {
		Plan         planOut `json:"plan"`
		DefaultProxy struct {
			Weight      int    `json:"weight"`
			ExitCountry string `json:"exit_country"`
			ExitIP      string `json:"exit_ip"`
			Books       int    `json:"books"`
			FirstBook   book   `json:"first_book"`
		} `json:"default_proxy"`
		PlanToken struct {
			Token         string `json:"token"`
			FirstCountry  string `json:"first_country"`
			SecondCountry string `json:"second_country"`
			SameIP        bool   `json:"same_ip"`
		} `json:"plan_token"`
	}{}
	out.Plan = planOut{sub.PlanID, sub.Threads, sub.Minutes, sub.MaxSess / 60, sub.Features.MaxPages, sub.Features.MemoryMB, options}
	out.DefaultProxy.Weight, out.DefaultProxy.ExitCountry, out.DefaultProxy.ExitIP, out.DefaultProxy.Books = weight, first.Country, first.IP, len(books)
	if len(books) > 0 {
		out.DefaultProxy.FirstBook = books[0]
	}
	out.PlanToken.Token = regexp.MustCompile(`session-\w+`).ReplaceAllString(token, "session-…")
	out.PlanToken.FirstCountry, out.PlanToken.SecondCountry, out.PlanToken.SameIP = a.Country, b.Country, a.IP == b.IP

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
