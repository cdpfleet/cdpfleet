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
	"strings"
	"unicode/utf16"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

var urls = []string{"https://news.ycombinator.com/", "https://en.wikipedia.org/wiki/Web_scraping", "https://books.toscrape.com/"}

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

type pageStats struct {
	URL        string  `json:"url"`
	HTMLChars  int     `json:"html_chars"`
	TextChars  int     `json:"text_chars"`
	AriaChars  int     `json:"aria_chars"`
	AriaLinks  int     `json:"aria_links"`
	AriaVsHTML float64 `json:"aria_vs_html"`
	AriaSample string  `json:"aria_sample"`
}

// chars counts UTF-16 code units, like String.length in JavaScript, Java and C#.
func chars(s string) int { return len(utf16.Encode([]rune(s))) }

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
	pages := []pageStats{}
	for _, url := range urls {
		if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(60000)}); err != nil {
			log.Fatal(err)
		}
		html, err := page.Content()
		if err != nil {
			log.Fatal(err)
		}
		text, err := page.Locator("body").InnerText()
		if err != nil {
			log.Fatal(err)
		}
		aria, err := page.Locator("body").AriaSnapshot()
		if err != nil {
			log.Fatal(err)
		}
		lines := strings.Split(aria, "\n")
		pages = append(pages, pageStats{
			URL:        url,
			HTMLChars:  chars(html),
			TextChars:  chars(text),
			AriaChars:  chars(aria),
			AriaLinks:  strings.Count(aria, "- link "),
			AriaVsHTML: math.Round(float64(chars(aria))/float64(chars(html))*1000) / 10,
			AriaSample: strings.Join(lines[:min(6, len(lines))], "\n"),
		})
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"pages": pages})
	fmt.Print(buf.String())
}
