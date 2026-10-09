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
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const start = "https://books.toscrape.com/"
const extractJS = `() => [...document.querySelectorAll('article.product_pod')].map(el => {
  const stars = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };
  const ratingClass = [...el.querySelector('.star-rating').classList].find(c => c !== 'star-rating');
  return {
    title: el.querySelector('h3 a').getAttribute('title'),
    price: parseFloat(el.querySelector('.price_color').textContent.replace(/[^0-9.]/g, '')),
    rating: stars[ratingClass] || 0,
    in_stock: el.querySelector('.availability').textContent.trim().toLowerCase().includes('in stock'),
  };
})`

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

type book struct {
	Title   string  `json:"title"`
	Price   float64 `json:"price"`
	Rating  int     `json:"rating"`
	InStock bool    `json:"in_stock"`
}

type output struct {
	PagesScraped int    `json:"pages_scraped"`
	TotalBooks   int    `json:"total_books"`
	Books        []book `json:"books"`
	Seconds      float64 `json:"seconds"`
}

func extractBooks(page playwright.Page) []book {
	v, err := page.Evaluate(extractJS)
	if err != nil {
		log.Fatal(err)
	}
	var books []book
	for _, item := range v.([]any) {
		m := item.(map[string]any)
		b := book{Title: m["title"].(string), InStock: m["in_stock"].(bool)}
		switch p := m["price"].(type) {
		case float64:
			b.Price = p
		}
		switch r := m["rating"].(type) {
		case float64:
			b.Rating = int(r)
		case int:
			b.Rating = r
		}
		books = append(books, b)
	}
	return books
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
	t := time.Now()

	if _, err := page.Goto(start, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}
	books := extractBooks(page)

	next := page.Locator("li.next a")
	count, err := next.Count()
	if err != nil {
		log.Fatal(err)
	}
	if count > 0 {
		if err := next.Click(); err != nil {
			log.Fatal(err)
		}
		if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateDomcontentloaded}); err != nil {
			log.Fatal(err)
		}
		books = append(books, extractBooks(page)...)
	}

	out := output{
		PagesScraped: 2,
		TotalBooks:   len(books),
		Books:        books,
		Seconds:      math.Round(time.Since(t).Seconds()*100) / 100,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
