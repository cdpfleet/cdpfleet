// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// The families a font-fingerprinting script typically tries: Windows, macOS, Linux and popular extras.
var candidates = []string{
	"Arial", "Arial Black", "Calibri", "Cambria", "Candara", "Comic Sans MS", "Consolas", "Constantia", "Corbel", "Courier New",
	"Georgia", "Impact", "Lucida Console", "Lucida Sans Unicode", "Microsoft Sans Serif", "Palatino Linotype", "Segoe UI", "Tahoma",
	"Times New Roman", "Trebuchet MS", "Verdana", "American Typewriter", "Avenir", "Avenir Next", "Futura", "Geneva", "Gill Sans",
	"Helvetica", "Helvetica Neue", "Lucida Grande", "Menlo", "Monaco", "Optima", "SF Pro", "DejaVu Sans", "DejaVu Serif",
	"Liberation Sans", "Liberation Mono", "Noto Sans", "Ubuntu", "Cantarell", "Roboto", "Open Sans", "Fira Code", "JetBrains Mono",
	"Source Code Pro",
}

// The classic width probe: a family is "installed" if text set in it, with a generic fallback,
// measures differently from the generic family alone.
const probe = `(fonts) => {
  const span = document.createElement('span');
  span.textContent = 'mmmmmmmmmmlli10OWQ@#';
  span.style.cssText = 'position:absolute;left:-9999px;font-size:72px';
  document.body.appendChild(span);
  const size = (family) => { span.style.fontFamily = family; return span.offsetWidth + 'x' + span.offsetHeight; };
  const generics = ['monospace', 'serif', 'sans-serif'];
  const base = {};
  for (const g of generics) base[g] = size(g);
  const found = fonts.filter((f) => generics.some((g) => size('"' + f + '", ' + g) !== base[g]));
  span.remove();
  return { platform: navigator.platform, found, generic_sizes: generics.map((g) => base[g]) };
}`

type setup struct {
	Name string
	Body map[string]any
}

var setups = []setup{
	{"os: windows (launch 1)", map[string]any{"os": "windows"}},
	{"os: windows (launch 2)", map[string]any{"os": "windows"}},
	{"os: macos", map[string]any{"os": "macos"}},
	{"os: linux", map[string]any{"os": "linux"}},
	{"windows + fonts", map[string]any{"os": "windows", "fonts": []string{"Menlo", "Helvetica Neue", "Fira Code"}}},
	{"windows + fonts + custom_fonts_only", map[string]any{"os": "windows", "fonts": []string{"Calibri", "Consolas", "Segoe UI", "Fira Code"}, "custom_fonts_only": true}},
}

type result struct {
	Setup             string   `json:"setup"`
	Platform          string   `json:"platform"`
	Detected          int      `json:"detected"`
	FontHash          string   `json:"font_hash"`
	GenericsIdentical bool     `json:"generics_identical"`
	Fonts             []string `json:"fonts"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/camoufox/session", bytes.NewReader(body))
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

// Proxies drop a tunnel now and then: up to two retries.
func goTo(page playwright.Page, url string) error {
	for attempt := 1; ; attempt++ {
		_, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil || attempt == 3 {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}

func one(pw *playwright.Playwright, s setup) (result, error) {
	out := result{Setup: s.Name, Fonts: []string{}}
	opts := map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true, "record": false}
	for k, v := range s.Body {
		opts[k] = v
	}
	session, err := launch(opts)
	if err != nil {
		return out, err
	}
	browser, err := pw.Firefox.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()
	context, err := browser.NewContext()
	if err != nil {
		return out, err
	}
	page, err := context.NewPage()
	if err != nil {
		return out, err
	}
	if err := goTo(page, "https://httpbin.org/html"); err != nil {
		return out, err
	}
	raw, err := page.Evaluate(probe, candidates)
	if err != nil {
		return out, err
	}
	r := raw.(map[string]any)
	out.Platform = r["platform"].(string)
	for _, f := range r["found"].([]any) {
		out.Fonts = append(out.Fonts, f.(string))
	}
	out.Detected = len(out.Fonts)
	h := sha256.Sum256([]byte(strings.Join(out.Fonts, ",")))
	out.FontHash = hex.EncodeToString(h[:])[:10]
	sizes := map[string]bool{}
	for _, v := range r["generic_sizes"].([]any) {
		sizes[v.(string)] = true
	}
	out.GenericsIdentical = len(sizes) == 1
	return out, nil
}

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
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		results = append(results, r)
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(b))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
