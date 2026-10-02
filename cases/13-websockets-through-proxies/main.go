// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const echoServer = "wss://ws.postman-echo.com/raw" // a public WebSocket echo server

// Runs in the page: connect, then 20 echo round trips, one at a time.
const probe = `async (url) => {
  const t0 = performance.now();
  const ws = new WebSocket(url);
  await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('WebSocket failed')); });
  const connectMs = performance.now() - t0;
  const rtts = [];
  for (let i = 0; i < 20; i++) {
    const sent = performance.now();
    const echoed = new Promise((ok) => { ws.onmessage = (m) => ok(m.data); });
    ws.send('ping ' + i);
    if ((await echoed) !== 'ping ' + i) throw new Error('wrong echo');
    rtts.push(performance.now() - sent);
  }
  ws.close();
  rtts.sort((a, b) => a - b);
  return { connectMs, p50: rtts[10], p95: rtts[18], min: rtts[0] };
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

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
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

func measureOnce(pw *playwright.Playwright, label, proxy string) (map[string]any, error) {
	session, err := launch("chromium", map[string]any{"proxy": proxy, "headless": true})
	if err != nil {
		return nil, err
	}
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return nil, err
	}
	defer browser.Close()
	page, _ := browser.NewPage()
	// Where this proxy exits (also gives the page an https origin to open the socket from).
	res, err := page.Goto("http://ip-api.com/json/?fields=country,city", playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
	if err != nil {
		return nil, err
	}
	var geo struct{ Country, City string }
	res.JSON(&geo)
	if _, err := page.Goto("https://httpbin.org/html", playwright.PageGotoOptions{Timeout: playwright.Float(30000)}); err != nil {
		return nil, err
	}
	v, err := page.Evaluate(probe, echoServer)
	if err != nil {
		return nil, err
	}
	r := v.(map[string]any)
	ms := func(k string) int64 { return int64(math.Round(num(r[k]))) }
	return map[string]any{"proxy": label, "exit": geo.City + ", " + geo.Country,
		"connect_ms": ms("connectMs"), "rtt_p50_ms": ms("p50"), "rtt_p95_ms": ms("p95"), "rtt_min_ms": ms("min")}, nil
}

// Residential exits drop a connection now and then: one retry in a new session.
func measure(pw *playwright.Playwright, label, proxy string) map[string]any {
	var err error
	for attempt := 1; attempt <= 2; attempt++ {
		var r map[string]any
		if r, err = measureOnce(pw, label, proxy); err == nil {
			return r
		}
	}
	return map[string]any{"proxy": label, "error": strings.SplitN(err.Error(), "\n", 2)[0]}
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	proxies := [][2]string{
		{"residential (any country)", os.Getenv("PROXY_URL")},
		{"residential (Germany)", os.Getenv("PROXY_URL_DE")},
		{"datacenter SOCKS5", strings.Split(os.Getenv("SOCKS_PROXIES"), ",")[0]},
	}
	rows := []map[string]any{}
	for _, p := range proxies {
		rows = append(rows, measure(pw, p[0], p[1]))
	}
	out, _ := json.MarshalIndent(map[string]any{"echo_server": echoServer, "round_trips": 20, "results": rows}, "", "  ")
	fmt.Println(string(out))
}
