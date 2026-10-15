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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const site = "https://shop.example.test" // doesn't exist: every request to it is answered by the script

// A tiny shop front: loads products from its API, shows an error state, and a slideshow title
// from a real public API (httpbin.org/json).
var html = `<html><head><meta charset="utf-8"></head><body><h1>Shop</h1><ul id="list"></ul><p id="status"></p><p id="title"></p>
<img src="` + site + `/banner.png"><img src="https://httpbin.org/image/png">
<script>
  fetch('/api/products').then((r) => r.ok ? r.json() : Promise.reject(r.status))
    .then((items) => { for (const p of items) document.getElementById('list').insertAdjacentHTML('beforeend', '<li>' + p.name + ' — ' + p.price + '</li>'); document.getElementById('status').textContent = 'ok'; })
    .catch((code) => { document.getElementById('status').textContent = 'error ' + code; });
  fetch('https://httpbin.org/json').then((r) => r.json()).then((j) => { document.getElementById('title').textContent = j.slideshow.title; });
</script></body></html>`

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

type shown struct {
	Items  []string `json:"items"`
	Status string   `json:"status"`
	Title  string   `json:"title"`
}

// Open the shop with the API answering api ("stub" or "error"); returns what the page shows.
func visit(context playwright.BrowserContext, api string) (shown, int, error) {
	var out shown
	page, err := context.NewPage()
	if err != nil {
		return out, 0, err
	}
	defer page.Close()
	aborted := 0
	// The page itself and its API never leave the browser (the page also declares its charset).
	page.Route(site+"/", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html; charset=utf-8"), Body: html})
	})
	page.Route(site+"/api/products", func(route playwright.Route) {
		if api == "stub" {
			// playwright-go has no JSON option: marshal the body and set the content type.
			products, _ := json.Marshal([]map[string]string{{"name": "Red mug", "price": "$9"}, {"name": "Blue mug", "price": "$11"}, {"name": "Teapot", "price": "$24"}})
			route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("application/json"), Body: products})
		} else {
			route.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(500), Body: "boom"})
		}
	})
	// A real response, changed on its way in.
	page.Route("https://httpbin.org/json", func(route playwright.Route) {
		response, err := route.Fetch()
		if err != nil {
			route.Abort()
			return
		}
		var data map[string]any
		body, _ := response.Body()
		json.Unmarshal(body, &data)
		show := data["slideshow"].(map[string]any)
		show["title"] = "Patched: " + show["title"].(string)
		patched, _ := json.Marshal(data)
		route.Fulfill(playwright.RouteFulfillOptions{Response: response, Body: patched})
	})
	// Images are dropped before they're requested.
	page.Route(regexp.MustCompile(`\.png$|/image/`), func(route playwright.Route) {
		aborted++
		route.Abort()
	})
	if _, err = page.Goto(site + "/"); err != nil {
		return out, 0, err
	}
	if _, err = page.WaitForFunction("() => document.getElementById('status').textContent && document.getElementById('title').textContent", nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(30000)}); err != nil {
		return out, 0, err
	}
	raw, err := page.Evaluate("() => ({ items: [...document.querySelectorAll('#list li')].map((li) => li.textContent), status: document.getElementById('status').textContent, title: document.getElementById('title').textContent })")
	if err != nil {
		return out, 0, err
	}
	b, _ := json.Marshal(raw)
	return out, aborted, json.Unmarshal(b, &out)
}

// Does the charset in the fulfilled Content-Type alone decode non-ASCII text? (No <meta>.)
func charsetHeaderOnly(context playwright.BrowserContext) (string, error) {
	page, err := context.NewPage()
	if err != nil {
		return "", err
	}
	defer page.Close()
	page.Route(site+"/plain", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html; charset=utf-8"), Body: "<p>Red mug — $9</p>"})
	})
	if _, err = page.Goto(site + "/plain"); err != nil {
		return "", err
	}
	return page.TextContent("p")
}

// Field order matches the other languages' output.
type result struct {
	Engine         string   `json:"engine"`
	Items          []string `json:"items"`
	Status         string   `json:"status"`
	Title          string   `json:"title"`
	ImagesAborted  int      `json:"images_aborted"`
	ErrorStatus    string   `json:"error_status"`
	HeaderOnlyText string   `json:"header_only_text"`
}

func one(pw *playwright.Playwright, engine string) (result, error) {
	out := result{Engine: engine}
	session, err := launch(engine, map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true, "record": false})
	if err != nil {
		return out, err
	}
	browserType := pw.Chromium
	switch engine {
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
	context, err := browser.NewContext()
	if err != nil {
		return out, err
	}
	stub, aborted, err := visit(context, "stub")
	if err != nil {
		return out, err
	}
	failed, _, err := visit(context, "error")
	if err != nil {
		return out, err
	}
	headerOnly, err := charsetHeaderOnly(context)
	if err != nil {
		return out, err
	}
	out.Items, out.Status, out.Title, out.ImagesAborted, out.ErrorStatus, out.HeaderOnlyText = stub.Items, stub.Status, stub.Title, aborted, failed.Status, headerOnly
	return out, nil
}

func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	results := []result{}
	for _, engine := range []string{"chromium", "firefox", "webkit"} {
		r, err := one(pw, engine)
		if err != nil {
			return fmt.Errorf("%s: %w", engine, err)
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
