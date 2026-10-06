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
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const start = "https://books.toscrape.com/" // 70-odd same-site links on the front page
const wave = 8                              // links checked at once
const linksJS = "(as, origin) => [...new Set(as.map(a => a.href))].filter(h => h.startsWith(origin) && !h.includes('#'))"
const fetchJS = "urls => Promise.all(urls.map(async url => {" +
	" try { const r = await fetch(url, { cache: 'no-store' }); return { url, status: r.status, redirected: r.redirected }; }" +
	" catch { return { url, status: 0, redirected: false }; } }))"

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

type result struct {
	URL        string
	Status     int
	Redirected bool
}

type row struct {
	Method         string   `json:"method"`
	Links          int      `json:"links"`
	OK             int      `json:"ok"`
	Redirected     int      `json:"redirected"`
	Broken         int      `json:"broken"`
	BrokenURLs     []string `json:"broken_urls"`
	Seconds        float64  `json:"seconds"`
	SecondsPerLink float64  `json:"seconds_per_link"`
	RequestsMade   int      `json:"requests_made"`
}

// Evaluate returns whole numbers as int and others as float64.
func num(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

func summary(method string, results []result, d time.Duration, requestsMade int) row {
	r := row{Method: method, Links: len(results), BrokenURLs: []string{}, RequestsMade: requestsMade}
	for _, x := range results {
		switch {
		case x.Status >= 200 && x.Status < 300:
			r.OK++
		}
		if x.Redirected {
			r.Redirected++
		}
		if x.Status == 0 || x.Status >= 400 {
			r.Broken++
			if len(r.BrokenURLs) < 5 {
				r.BrokenURLs = append(r.BrokenURLs, x.URL)
			}
		}
	}
	r.Seconds = math.Round(d.Seconds()*1000) / 1000
	r.SecondsPerLink = math.Round(d.Seconds()/float64(len(results))*100) / 100
	return r
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		log.Fatal(err)
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	if _, err := page.Goto(start, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}
	// Every unique same-site link on the page, as absolute URLs.
	u, _ := url.Parse(start)
	v, err := page.EvalOnSelectorAll("a[href]", linksJS, u.Scheme+"://"+u.Host)
	if err != nil {
		log.Fatal(err)
	}
	var links []string
	for _, l := range v.([]any) {
		links = append(links, l.(string))
	}

	// Way 1: fetch() inside the page, wave links at a time — the browser's proxy, cookies
	// and TLS, no navigation, no rendering, no assets.
	t1 := time.Now()
	var fetched []result
	for i := 0; i < len(links); i += wave {
		end := i + wave
		if end > len(links) {
			end = len(links)
		}
		v, err := page.Evaluate(fetchJS, links[i:end])
		if err != nil {
			log.Fatal(err)
		}
		for _, x := range v.([]any) {
			m := x.(map[string]any)
			fetched = append(fetched, result{URL: m["url"].(string), Status: num(m["status"]), Redirected: m["redirected"].(bool)})
		}
	}
	inPage := summary(fmt.Sprintf("fetch() in the page, %d at a time", wave), fetched, time.Since(t1), len(links))

	// Way 2: navigate to each link, like a user — full page loads with all their assets.
	// Only a sample: this is the slow way, and it is the same work for every link.
	sample := links
	if len(sample) > 10 {
		sample = sample[:10]
	}
	var mu sync.Mutex
	requests := 0
	page.OnRequest(func(playwright.Request) { mu.Lock(); requests++; mu.Unlock() })
	t2 := time.Now()
	var navigated []result
	for _, link := range sample {
		r, err := page.Goto(link, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err != nil || r == nil {
			navigated = append(navigated, result{URL: link})
			continue
		}
		navigated = append(navigated, result{URL: link, Status: r.Status(), Redirected: r.URL() != link})
	}
	mu.Lock()
	n := requests
	mu.Unlock()
	byNav := summary("page.goto each link (10-link sample)", navigated, time.Since(t2), n)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode([]row{inPage, byNav})
	fmt.Print(buf.String())
}
