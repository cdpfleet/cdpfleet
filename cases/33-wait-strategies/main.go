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
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// A static catalogue, a big article and a page that renders its data with a delayed script.
var pages = []struct{ url, data string }{
	{"https://books.toscrape.com/", "article.product_pod"},
	{"https://en.wikipedia.org/wiki/Web_scraping", "#mw-content-text p"},
	{"https://quotes.toscrape.com/js-delayed/", ".quote"},
}
var strategies = []string{"commit", "domcontentloaded", "load", "networkidle", "selector"}
var waitUntil = map[string]*playwright.WaitUntilState{
	"commit":           playwright.WaitUntilStateCommit,
	"domcontentloaded": playwright.WaitUntilStateDomcontentloaded,
	"load":             playwright.WaitUntilStateLoad,
	"networkidle":      playwright.WaitUntilStateNetworkidle,
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
	URL        string `json:"url"`
	Strategy   string `json:"strategy"`
	Ms         int64  `json:"ms"`
	ItemsReady int    `json:"items_ready"`
}

func measure(browser playwright.Browser, url, data, strategy string) (row, error) {
	// A fresh context each time: no cache, so every strategy waits for the same work.
	context, err := browser.NewContext()
	if err != nil {
		return row{}, err
	}
	defer context.Close()
	page, err := context.NewPage()
	if err != nil {
		return row{}, err
	}
	t := time.Now()
	if strategy == "selector" {
		if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateCommit, Timeout: playwright.Float(60000)}); err != nil {
			return row{}, err
		}
		if err := page.Locator(data).First().WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(60000)}); err != nil {
			return row{}, err
		}
	} else if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: waitUntil[strategy], Timeout: playwright.Float(60000)}); err != nil {
		return row{}, err
	}
	ms := time.Since(t).Milliseconds()
	count, err := page.Locator(data).Count()
	if err != nil {
		return row{}, err
	}
	return row{URL: url, Strategy: strategy, Ms: ms, ItemsReady: count}, nil
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

	rows := []row{}
	for _, p := range pages {
		for _, strategy := range strategies {
			r, err := measure(browser, p.url, p.data, strategy)
			if err != nil {
				browser.Close()
				log.Fatal(err)
			}
			rows = append(rows, r)
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"rows": rows})
	fmt.Print(buf.String())
}
