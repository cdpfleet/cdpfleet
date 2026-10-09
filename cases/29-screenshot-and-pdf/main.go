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
	"path/filepath"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const pageURL = "https://books.toscrape.com/"

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

type capture struct {
	Type          string  `json:"type"`
	FileSizeBytes int64   `json:"file_size_bytes"`
	Seconds       float64 `json:"seconds"`
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

	dir, err := os.MkdirTemp("", "cdpfleet-captures-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	page, err := browser.NewPage(playwright.BrowserNewPageOptions{ViewportSize: &playwright.Size{Width: 1280, Height: 720}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{Timeout: playwright.Float(60000), WaitUntil: playwright.WaitUntilStateNetworkidle}); err != nil {
		log.Fatal(err)
	}

	var results []capture

	snap := func(label string, fn func(string)) capture {
		ext := ".png"
		if label == "pdf" {
			ext = ".pdf"
		}
		path := filepath.Join(dir, label+ext)
		t := time.Now()
		fn(path)
		info, err := os.Stat(path)
		if err != nil {
			log.Fatal(err)
		}
		return capture{Type: label, FileSizeBytes: info.Size(), Seconds: math.Round(time.Since(t).Seconds()*100) / 100}
	}

	results = append(results, snap("viewport screenshot", func(p string) {
		if _, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(p)}); err != nil {
			log.Fatal(err)
		}
	}))
	results = append(results, snap("full-page screenshot", func(p string) {
		if _, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(p), FullPage: playwright.Bool(true)}); err != nil {
			log.Fatal(err)
		}
	}))
	results = append(results, snap("element screenshot", func(p string) {
		if _, err := page.Locator(".product_pod").First().Screenshot(playwright.LocatorScreenshotOptions{Path: playwright.String(p)}); err != nil {
			log.Fatal(err)
		}
	}))
	results = append(results, snap("pdf", func(p string) {
		if _, err := page.PDF(playwright.PagePdfOptions{Path: playwright.String(p)}); err != nil {
			log.Fatal(err)
		}
	}))

	out := struct {
		Captures []capture `json:"captures"`
	}{Captures: results}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
