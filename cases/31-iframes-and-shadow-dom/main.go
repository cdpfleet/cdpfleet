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
	"strings"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// Content a plain document.querySelector can't see: a same-origin iframe, a cross-origin
// iframe, an open shadow root (with another one nested inside) and a closed shadow root.
const html = `<!doctype html><title>Hidden content</title>
<h1>Main document</h1>
<iframe id="same" srcdoc="<p id='inner'>same-origin iframe text</p>"></iframe>
<iframe id="cross" src="https://httpbin.org/html"></iframe>
<open-card></open-card>
<closed-card></closed-card>
<script>
customElements.define('open-card', class extends HTMLElement {
  connectedCallback() {
    const root = this.attachShadow({ mode: 'open' });
    root.innerHTML = '<p class="msg">open shadow text</p><nested-badge></nested-badge>';
  }
});
customElements.define('nested-badge', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<span class="badge">nested shadow text</span>'; }
});
customElements.define('closed-card', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'closed' }).innerHTML = '<p class="secret">closed shadow text</p>'; }
});
</script>`

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
	Target        string  `json:"target"`
	QuerySelector *string `json:"querySelector"`
	Playwright    *string `json:"playwright"`
	How           string  `json:"how"`
}

type crossFrame struct {
	URL   *string `json:"url"`
	Title *string `json:"title"`
}

type output struct {
	Frames           int        `json:"frames"`
	CrossOriginFrame crossFrame `json:"cross_origin_frame"`
	Rows             []row      `json:"rows"`
}

// qs runs a plain document.querySelector in the top document.
func qs(page playwright.Page, sel string) *string {
	v, err := page.Evaluate("(s) => document.querySelector(s)?.textContent ?? null", sel)
	if err != nil {
		log.Fatal(err)
	}
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// pw returns the first match's text, or nil if the locator finds nothing.
func pw(loc playwright.Locator) *string {
	n, err := loc.Count()
	if err != nil {
		log.Fatal(err)
	}
	if n == 0 {
		return nil
	}
	s, err := loc.First().TextContent()
	if err != nil {
		log.Fatal(err)
	}
	return &s
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	if err != nil {
		log.Fatal(err)
	}
	pwr, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pwr.Stop()
	browser, err := pwr.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	if err := page.SetContent(html); err != nil {
		log.Fatal(err)
	}
	if err := page.FrameLocator("#cross").Locator("h1").WaitFor(playwright.LocatorWaitForOptions{Timeout: playwright.Float(60000)}); err != nil {
		log.Fatal(err)
	}

	rows := []row{
		{"same-origin iframe", qs(page, "#inner"), pw(page.FrameLocator("#same").Locator("#inner")), "page.frameLocator('#same').locator('#inner')"},
		{"cross-origin iframe", qs(page, "h1 + div p"), pw(page.FrameLocator("#cross").Locator("h1")), "page.frameLocator('#cross').locator('h1')"},
		{"open shadow root", qs(page, ".msg"), pw(page.Locator(".msg")), "page.locator('.msg') — CSS pierces open shadow roots"},
		{"nested open shadow root", qs(page, ".badge"), pw(page.Locator(".badge")), "page.locator('.badge') — any depth"},
		{"closed shadow root", qs(page, ".secret"), pw(page.Locator(".secret")), "not reachable from page scripts or locators"},
	}

	// The cross-origin frame is a separate document: its URL and title come from the frame object.
	out := output{Frames: len(page.Frames()), Rows: rows}
	for _, f := range page.Frames() {
		if strings.HasPrefix(f.URL(), "https://httpbin.org") {
			url := f.URL()
			title, err := f.Title()
			if err != nil {
				log.Fatal(err)
			}
			out.CrossOriginFrame = crossFrame{URL: &url, Title: &title}
			break
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
