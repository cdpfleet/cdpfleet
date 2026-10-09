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
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

var urls = []string{
	"https://books.toscrape.com/",
	"https://quotes.toscrape.com/",
	"https://example.com",
	"https://httpbin.org/html",
	"https://www.scrapethissite.com/",
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

type result struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Method string `json:"method"`
}

func extract(browser playwright.Browser, url string) (string, string) {
	page, err := browser.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	defer page.Close()
	if _, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}
	title, err := page.Title()
	if err != nil {
		log.Fatal(err)
	}
	return url, title
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

	t1 := time.Now()
	var sequential []result
	for _, u := range urls {
		url, title := extract(browser, u)
		sequential = append(sequential, result{URL: url, Title: title, Method: "sequential"})
	}
	seqSeconds := math.Round(time.Since(t1).Seconds()*100) / 100

	t2 := time.Now()
	parallel := make([]result, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			url, title := extract(browser, u)
			parallel[i] = result{URL: url, Title: title, Method: "parallel"}
		}(i, u)
	}
	wg.Wait()
	parSeconds := math.Round(time.Since(t2).Seconds()*100) / 100

	all := append(sequential, parallel...)
	out := struct {
		SequentialSeconds float64  `json:"sequential_seconds"`
		ParallelSeconds   float64  `json:"parallel_seconds"`
		Speedup           string   `json:"speedup"`
		Results           []result `json:"results"`
	}{
		SequentialSeconds: seqSeconds,
		ParallelSeconds:   parSeconds,
		Speedup:           fmt.Sprintf("%.1fx", math.Round(seqSeconds/parSeconds*10)/10),
		Results:           all,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
