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
	Variant        string   `json:"variant"`
	HeaderOrder    []string `json:"header_order"`
	AcceptLanguage *string  `json:"accept_language"`
	AkamaiH2       string   `json:"akamai_h2"`
	JA4            string   `json:"ja4"`
}

// What the server saw: header names in order, and the HTTP/2 fingerprint.
func observe(browser playwright.Browser, label string, options playwright.BrowserNewContextOptions, setup func(playwright.Page)) row {
	context, err := browser.NewContext(options)
	if err != nil {
		log.Fatal(err)
	}
	page, _ := context.NewPage()
	if setup != nil {
		setup(page)
	}
	res, err := page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	var fp struct {
		TLS struct {
			JA4 string `json:"ja4"`
		} `json:"tls"`
		HTTP2 struct {
			AkamaiFingerprint string `json:"akamai_fingerprint"`
			SentFrames        []struct {
				FrameType string   `json:"frame_type"`
				Headers   []string `json:"headers"`
			} `json:"sent_frames"`
		} `json:"http2"`
	}
	res.JSON(&fp)
	context.Close()
	r := row{Variant: label, HeaderOrder: []string{}, AkamaiH2: fp.HTTP2.AkamaiFingerprint, JA4: fp.TLS.JA4}
	for _, f := range fp.HTTP2.SentFrames {
		if f.FrameType != "HEADERS" {
			continue
		}
		for _, h := range f.Headers {
			r.HeaderOrder = append(r.HeaderOrder, h[:strings.Index(h[1:], ":")+1])
			if strings.HasPrefix(h, "accept-language: ") {
				v := h[17:]
				r.AcceptLanguage = &v
			}
		}
	}
	return r
}

func main() {
	session, err := launch("chrome", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": false})
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

	rows := []row{
		observe(browser, "default", playwright.BrowserNewContextOptions{}, nil),
		observe(browser, "locale: de-DE", playwright.BrowserNewContextOptions{Locale: playwright.String("de-DE")}, nil),
		observe(browser, "extraHTTPHeaders", playwright.BrowserNewContextOptions{
			ExtraHttpHeaders: map[string]string{"accept-language": "de-DE", "x-request-id": "abc123"}}, nil),
		observe(browser, "route: rewrite headers", playwright.BrowserNewContextOptions{}, func(page playwright.Page) {
			page.Route("**/*", func(route playwright.Route) {
				headers := route.Request().Headers()
				headers["x-request-id"] = "abc123"
				route.Continue(playwright.RouteContinueOptions{Headers: headers})
			})
		}),
	}
	out, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Println(string(out))
}
