// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// What a page can learn about where the visitor is, from JavaScript.
const probe = `async () => {
  const geo = await new Promise((resolve) => navigator.geolocation.getCurrentPosition(
    (p) => resolve({ lat: Math.round(p.coords.latitude * 100) / 100, lon: Math.round(p.coords.longitude * 100) / 100 }),
    (e) => resolve({ error: e.code === 1 ? 'permission denied' : e.message }),
    { timeout: 5000 }));
  const perm = (await navigator.permissions.query({ name: 'geolocation' })).state;
  return { geolocation: geo, permission: perm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, language: navigator.language, utc_offset_min: -new Date().getTimezoneOffset() };
}`

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

type ipInfo struct {
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
}

type contextResult struct {
	Setup        string `json:"setup"`
	Geolocation  any    `json:"geolocation"`
	Permission   any    `json:"permission"`
	Timezone     any    `json:"timezone"`
	Language     any    `json:"language"`
	UTCOffsetMin any    `json:"utc_offset_min"`
}

type output struct {
	IP       *ipInfo         `json:"ip"`
	Contexts []contextResult `json:"contexts"`
}

type setup struct {
	label   string
	options playwright.BrowserNewContextOptions
}

func run(browser playwright.Browser) (output, error) {
	out := output{Contexts: []contextResult{}}
	setups := []setup{
		{"defaults", playwright.BrowserNewContextOptions{}},
		{"permission granted, no position", playwright.BrowserNewContextOptions{Permissions: []string{"geolocation"}}},
		{"Berlin: geolocation + timezone + locale", playwright.BrowserNewContextOptions{
			Permissions: []string{"geolocation"},
			Geolocation: &playwright.Geolocation{Latitude: 52.52, Longitude: 13.405},
			TimezoneId:  playwright.String("Europe/Berlin"),
			Locale:      playwright.String("de-DE"),
		}},
	}
	for _, s := range setups {
		ctx, err := browser.NewContext(s.options)
		if err != nil {
			return out, err
		}
		page, err := ctx.NewPage()
		if err != nil {
			return out, err
		}
		if out.IP == nil {
			if _, err := page.Goto("http://ip-api.com/json/?fields=query,countryCode,timezone", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
				return out, err
			}
			text, err := page.TextContent("body")
			if err != nil {
				return out, err
			}
			var ip struct {
				CountryCode string `json:"countryCode"`
				Timezone    string `json:"timezone"`
			}
			if err := json.Unmarshal([]byte(text), &ip); err != nil {
				return out, err
			}
			out.IP = &ipInfo{Country: ip.CountryCode, Timezone: ip.Timezone}
		}
		if _, err := page.Goto("https://httpbin.org/html", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
			return out, err
		}
		v, err := page.Evaluate("(" + probe + ")()")
		if err != nil {
			return out, err
		}
		m := v.(map[string]any)
		out.Contexts = append(out.Contexts, contextResult{
			Setup:        s.label,
			Geolocation:  m["geolocation"],
			Permission:   m["permission"],
			Timezone:     m["timezone"],
			Language:     m["language"],
			UTCOffsetMin: m["utc_offset_min"],
		})
		if err := ctx.Close(); err != nil {
			return out, err
		}
	}
	return out, nil
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL_DE"), "headless": "new"})
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

	out, err := run(browser)
	browser.Close()
	if err != nil {
		log.Fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
}
