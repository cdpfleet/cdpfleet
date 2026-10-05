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
	"regexp"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"

// Three pages, one question each: is the data in the HTML, or does it need JavaScript?
type target struct {
	url, item string
	expected  int
}

var targets = []target{
	{"https://books.toscrape.com/", "product_pod", 20},
	{"https://quotes.toscrape.com/js/", "quote", 10},
	{"https://quotes.toscrape.com/scroll", "quote", 10},
}

type row struct {
	URL            string   `json:"url"`
	HTTPStatus     int      `json:"http_status"`
	HTMLKB         int      `json:"html_kb"`
	ItemsInHTML    int      `json:"items_in_html"`
	FetchSeconds   float64  `json:"fetch_seconds"`
	ItemsInBrowser *int     `json:"items_in_browser"`
	BrowserSeconds *float64 `json:"browser_seconds"`
	NeedsBrowser   bool     `json:"needs_browser"`
}

func seconds(d time.Duration) float64 { return math.Round(d.Seconds()*1000) / 1000 }

func countInHTML(html, cls string) int {
	return len(regexp.MustCompile(`class="[^"]*\b`+regexp.QuoteMeta(cls)+`\b[^"]*"`).FindAllStringIndex(html, -1))
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

func main() {
	proxyURL := os.Getenv("PROXY_URL")
	proxy, err := url.Parse(proxyURL)
	if err != nil {
		log.Fatal(err)
	}
	pass, _ := proxy.User.Password()
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()

	// Step 1: a plain HTTP GET through the same proxy, no browser (Playwright's request API
	// runs locally; the proxy keeps the exit IP identical to the browser's).
	httpCtx, err := pw.Request.NewContext(playwright.APIRequestNewContextOptions{
		Proxy:     &playwright.Proxy{Server: proxy.Scheme + "://" + proxy.Host, Username: playwright.String(proxy.User.Username()), Password: playwright.String(pass)},
		UserAgent: playwright.String(ua),
	})
	if err != nil {
		log.Fatal(err)
	}
	rows := make([]*row, len(targets))
	for i, t := range targets {
		t0 := time.Now()
		res, err := httpCtx.Get(t.url, playwright.APIRequestContextGetOptions{Timeout: playwright.Float(60000)})
		if err != nil {
			log.Fatal(err)
		}
		html, err := res.Text()
		if err != nil {
			log.Fatal(err)
		}
		rows[i] = &row{URL: t.url, HTTPStatus: res.Status(), HTMLKB: int(math.Round(float64(len(html)) / 1024)),
			ItemsInHTML: countInHTML(html, t.item), FetchSeconds: seconds(time.Since(t0))}
	}
	httpCtx.Dispose()

	// Step 2: only the pages whose HTML didn't have the items get a browser.
	var needsBrowser []int
	for i, r := range rows {
		if r.ItemsInHTML < targets[i].expected {
			needsBrowser = append(needsBrowser, i)
		}
	}
	if len(needsBrowser) > 0 {
		session, err := launch("chromium", map[string]any{"proxy": proxyURL, "headless": "new"})
		if err != nil {
			log.Fatal(err)
		}
		browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		if err != nil {
			log.Fatal(err)
		}
		for _, i := range needsBrowser {
			t, r := targets[i], rows[i]
			page, err := browser.NewPage()
			if err != nil {
				log.Fatal(err)
			}
			t0 := time.Now()
			if _, err := page.Goto(t.url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
				log.Fatal(err)
			}
			if err := page.Locator("."+t.item).First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(60000)}); err != nil {
				log.Fatal(err)
			}
			n, err := page.Locator("." + t.item).Count()
			if err != nil {
				log.Fatal(err)
			}
			secs := seconds(time.Since(t0))
			r.ItemsInBrowser, r.BrowserSeconds, r.NeedsBrowser = &n, &secs, true
			page.Close()
		}
		browser.Close()
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(rows)
	fmt.Print(buf.String())
}
