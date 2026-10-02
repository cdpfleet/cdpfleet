// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_US, PROXY_URL_DE, PROXY_URL_JP (exits in each country)
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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// What a localizing site reads in the page. Run in the PAGE's own JavaScript world
// ("mw:" prefix, needs main_world_eval): Playwright's default isolated world isn't patched
// the same way and can report the server's UTC timezone instead of the persona's.
const localView = `(async () => {
  const position = await new Promise((ok) => navigator.geolocation.getCurrentPosition(
    (p) => ok({ lat: p.coords.latitude, lon: p.coords.longitude }), () => ok(null), { timeout: 10000 }));
  return {
    languages: navigator.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    date: new Date('2026-10-01T15:30:00Z').toLocaleString(),
    number: (1234567.891).toLocaleString(),
    price: new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(49.9),
    position,
  };
})()`

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

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// Great-circle distance in km.
func km(lat1, lon1, lat2, lon2 float64) int64 {
	r := func(d float64) float64 { return d * math.Pi / 180 }
	h := math.Pow(math.Sin(r(lat2-lat1)/2), 2) + math.Cos(r(lat1))*math.Cos(r(lat2))*math.Pow(math.Sin(r(lon2-lon1)/2), 2)
	return int64(math.Round(12742 * math.Asin(math.Sqrt(h))))
}

// Evaluate returns whole numbers as int and others as float64.
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func persona(pw *playwright.Playwright, country, locale, proxy string) map[string]any {
	fail := func(err error) map[string]any { return map[string]any{"country": country, "error": err.Error()} }
	// geoip: Camoufox sets timezone and geolocation from the proxy's exit IP at launch.
	session, err := launch("camoufox", map[string]any{"proxy": proxy, "headless": true, "os": "windows", "locale": locale, "geoip": true, "main_world_eval": true})
	if err != nil {
		return fail(err)
	}
	browser, err := pw.Firefox.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fail(err)
	}
	defer browser.Close()
	context, _ := browser.NewContext()
	context.GrantPermissions([]string{"geolocation"}) // as if the visitor clicked "Allow"
	page, _ := context.NewPage()
	res, err := page.Goto("http://ip-api.com/json/?fields=country,city,timezone,lat,lon", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		return fail(err)
	}
	var exit struct {
		Country, City, Timezone string
		Lat, Lon                float64
	}
	res.JSON(&exit)
	if _, err := page.Goto("https://httpbin.org/html", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return fail(err)
	}
	v, err := page.Evaluate("mw:" + localView)
	if err != nil {
		return fail(err)
	}
	isolated, _ := page.Evaluate("Intl.DateTimeFormat().resolvedOptions().timeZone")
	out := v.(map[string]any)
	out["country"], out["locale"], out["exit"], out["exit_timezone"] = country, locale, exit.City+", "+exit.Country, exit.Timezone
	out["position_km_from_exit"] = nil
	if pos, ok := out["position"].(map[string]any); ok {
		out["position_km_from_exit"] = km(num(pos["lat"]), num(pos["lon"]), exit.Lat, exit.Lon)
	}
	out["isolated_world_timezone"] = isolated // what a default page.evaluate would have reported
	return out
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	countries := [][3]string{
		{"United States", "en-US", os.Getenv("PROXY_URL_US")},
		{"Germany", "de-DE", os.Getenv("PROXY_URL_DE")},
		{"Japan", "ja-JP", os.Getenv("PROXY_URL_JP")},
	}
	rows := make([]map[string]any, len(countries))
	var wg sync.WaitGroup
	for i, c := range countries {
		wg.Add(1)
		go func(i int, c [3]string) { defer wg.Done(); rows[i] = persona(pw, c[0], c[1], c[2]) }(i, c)
	}
	wg.Wait()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(rows)
	fmt.Print(buf.String())
}
