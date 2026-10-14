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

// Which browsers can print a page to PDF over a remote connection, headless and headful.
type setup struct {
	label, engine, family string
	headless              any
}

var setups = []setup{
	{"Chromium, headless", "chromium", "chromium", true},
	{"Chrome, headless", "chrome", "chromium", "new"},
	{"Chrome, headful", "chrome", "chromium", false},
	{"Edge, headful", "edge", "chromium", false},
	{"Firefox, headful", "firefox", "firefox", false},
	{"Camoufox, headful", "camoufox", "firefox", false},
	{"WebKit, headful", "webkit", "webkit", false},
}

// A page with a coloured background, a screen-only banner hidden by print CSS and enough text
// for several printed pages. SetContent: no proxy traffic, the same page everywhere.
func html() string {
	var paras strings.Builder
	for i := 1; i <= 120; i++ {
		fmt.Fprintf(&paras, "<p>Paragraph %d: lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>", i)
	}
	return "<html><head><style>" +
		"body { font: 14px sans-serif; background: #e8f0ff; }" +
		".screen-only { background: #c00; color: #fff; padding: 8px; }" +
		"@media print { .screen-only { display: none; } }" +
		`</style></head><body><div class="screen-only">Cookie banner (screen only)</div><h1>Quarterly report</h1>` +
		paras.String() + "</body></html>"
}

// Go's regexp has no lookahead: match "/Type /Page" plus the next byte and drop "/Pages".
var pageRe = regexp.MustCompile(`(?s)/Type\s*/Page(.)`)

func pagesIn(pdf []byte) int {
	n := 0
	for _, m := range pageRe.FindAllSubmatch(pdf, -1) {
		if string(m[1]) != "s" {
			n++
		}
	}
	return n
}

// Field order matches the other languages' output.
type result struct {
	Label               string   `json:"label"`
	Pdf                 string   `json:"pdf"`
	PdfBytes            *int     `json:"pdf_bytes,omitempty"`
	PdfPages            *int     `json:"pdf_pages,omitempty"`
	WithBackgroundBytes *int     `json:"with_background_bytes,omitempty"`
	PdfSeconds          *float64 `json:"pdf_seconds,omitempty"`
	Error               string   `json:"error,omitempty"`
	PrintCSSApplied     bool     `json:"print_css_applied"`
	PrintScreenshotKB   int      `json:"print_screenshot_kb"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
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
			return nil, fmt.Errorf("launch %s %d %s", engine, res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func one(pw *playwright.Playwright, s setup) (result, error) {
	out := result{Label: s.label}
	session, err := launch(s.engine, map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": s.headless})
	if err != nil {
		return out, err
	}
	browserType := pw.Chromium
	switch s.family {
	case "firefox":
		browserType = pw.Firefox
	case "webkit":
		browserType = pw.WebKit
	}
	browser, err := browserType.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return out, err
	}
	if err := page.SetContent(html()); err != nil {
		return out, err
	}
	t := time.Now()
	pdf, err := page.PDF(playwright.PagePdfOptions{Format: playwright.String("A4")})
	if err == nil {
		withBg, err2 := page.PDF(playwright.PagePdfOptions{Format: playwright.String("A4"), PrintBackground: playwright.Bool(true)})
		if err2 != nil {
			return out, err2
		}
		bytesN, pages, bg := len(pdf), pagesIn(pdf), len(withBg)
		secs := math.Round(time.Since(t).Seconds()*100) / 100
		out.Pdf, out.PdfBytes, out.PdfPages, out.WithBackgroundBytes, out.PdfSeconds = "yes", &bytesN, &pages, &bg, &secs
	} else { // Firefox, Camoufox and WebKit refuse
		msg := strings.Split(err.Error(), "\n")[0]
		if len(msg) > 120 {
			msg = msg[:120]
		}
		out.Pdf, out.Error = "no", msg
	}
	// The fallback that works everywhere: print CSS + a full-page screenshot.
	if err := page.EmulateMedia(playwright.PageEmulateMediaOptions{Media: playwright.MediaPrint}); err != nil {
		return out, err
	}
	applied, err := page.Evaluate("() => getComputedStyle(document.querySelector('.screen-only')).display === 'none'")
	if err != nil {
		return out, err
	}
	out.PrintCSSApplied = applied == true
	shot, err := page.Screenshot(playwright.PageScreenshotOptions{FullPage: playwright.Bool(true)})
	if err != nil {
		return out, err
	}
	out.PrintScreenshotKB = int(math.Round(float64(len(shot)) / 1024))
	return out, nil
}

// run returns errors instead of exiting, so deferred Close and Stop calls still run.
func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	results := []result{}
	for _, s := range setups {
		r, err := one(pw, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
		results = append(results, r)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"results": results}); err != nil {
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
