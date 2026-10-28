// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (residential exits come from cdpfleet-resi tokens)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var run = func() string { s := strconv.FormatInt(time.Now().UnixMilli(), 36); return s[len(s)-4:] }()

type country struct{ Code, Locale string }
type setup struct {
	Label, Engine string
	Options       map[string]any
	Match         bool // context locale + timezone from a first lookup of the exit
}

var countries = []country{{"US", "en-US"}, {"DE", "de-DE"}, {"JP", "ja-JP"}}
var setups = []setup{
	{"Camoufox (geoip on, default)", "camoufox", map[string]any{"geoip": true}, false},
	{"Camoufox, geoip: false", "camoufox", map[string]any{"geoip": false}, false},
	{`Camoufox, locale: "pt-BR"`, "camoufox", map[string]any{"geoip": true, "locale": "pt-BR"}, false},
	{"Chrome headful, no locale", "chrome", map[string]any{"headless": false}, false},
	{"Chrome headful, context locale + timezoneId from the exit", "chrome", map[string]any{"headless": false}, true},
}

// What a site can read and compare, run on the exit-lookup page itself (ip-api.com's JSON, so the
// page knows the exit): exit country and timezone, languages, Accept-Language (echoed by httpbin),
// Intl's timezone and locale, a formatted number, and the WebRTC public address (compared with the
// exit, never returned).
const check = `(async () => {
  const exit = JSON.parse(document.body.innerText);
  const headers = await fetch('https://httpbin.org/headers').then((r) => r.json()).catch(() => ({ headers: {} }));
  const srflx = await new Promise((done) => {
    const ips = new Set();
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => { const c = e.candidate && e.candidate.candidate; if (c && c.includes(' srflx ')) ips.add(c.split(' ')[4]); };
      pc.createOffer().then((o) => pc.setLocalDescription(o));
      setTimeout(() => { pc.close(); done([...ips]); }, 4000);
    } catch { done([]); }
  });
  const intl = Intl.DateTimeFormat().resolvedOptions();
  const acceptLanguage = (headers.headers['Accept-Language'] || '').split(',')[0];
  const out = {
    exit_country: exit.countryCode, exit_timezone: exit.timezone,
    languages: navigator.languages.join(', '), accept_language: acceptLanguage,
    timezone: intl.timeZone, intl_locale: intl.locale, number: (1234567.891).toLocaleString(),
    webrtc: srflx.length === 0 ? 'none' : srflx.every((ip) => ip === exit.query) ? 'exit IP' : 'another IP',
  };
  out.mismatches = [
    out.timezone !== exit.timezone && 'timezone ≠ IP timezone',
    (navigator.language.split('-')[1] || '').toUpperCase() !== exit.countryCode && 'language region ≠ IP country',
    acceptLanguage.toLowerCase() !== navigator.language.toLowerCase() && 'Accept-Language ≠ navigator.language',
    !intl.locale.toLowerCase().startsWith(navigator.language.slice(0, 2).toLowerCase()) && 'Intl locale ≠ navigator.language',
    out.webrtc === 'another IP' && 'WebRTC shows another IP',
  ].filter(Boolean);
  return out;
})()`

func launch(engine string, body map[string]any) (string, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(b))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("launch %d %.120s", res.StatusCode, msg)
	}
	var s struct{ WsUrl string }
	return s.WsUrl, json.NewDecoder(res.Body).Decode(&s)
}

func measure(pw *playwright.Playwright, c country, s setup) map[string]any {
	cc := strings.ToLower(c.Code)
	row := map[string]any{"setup": s.Label, "country": c.Code}
	fail := func(err error) map[string]any { row["error"] = strings.Split(err.Error(), "\n")[0]; return row }
	camoufox := s.Engine == "camoufox"
	body := map[string]any{"proxy": "cdpfleet-resi-country-" + cc + "-session-c74" + cc + run + "-lifetime-10", "headless": camoufox, "record": false}
	if camoufox {
		body["main_world_eval"] = true
	}
	for k, v := range s.Options {
		body[k] = v
	}
	wsUrl, err := launch(s.Engine, body)
	if err != nil {
		return fail(err)
	}
	bt := pw.Chromium
	if camoufox {
		bt = pw.Firefox
	}
	browser, err := bt.Connect(wsUrl, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fail(err)
	}
	defer browser.Close()
	opts := playwright.BrowserNewContextOptions{}
	if s.Match { // look the exit up first, then set the context to match it
		probe, err := browser.NewContext()
		if err != nil {
			return fail(err)
		}
		pg, _ := probe.NewPage()
		if _, err := pg.Goto("http://ip-api.com/json/?fields=timezone", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
			return fail(err)
		}
		text, _ := pg.TextContent("body")
		var tz struct{ Timezone string }
		json.Unmarshal([]byte(text), &tz)
		opts.Locale, opts.TimezoneId = playwright.String(c.Locale), playwright.String(tz.Timezone)
		probe.Close()
	}
	context, err := browser.NewContext(opts)
	if err != nil {
		return fail(err)
	}
	page, _ := context.NewPage()
	if _, err := page.Goto("http://ip-api.com/json/?fields=countryCode,timezone,query", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return fail(err)
	}
	expr := check
	if camoufox {
		expr = "mw:" + check
	}
	v, err := page.Evaluate(expr)
	if err != nil {
		return fail(err)
	}
	for k, val := range v.(map[string]any) {
		row[k] = val
	}
	return row
}

func main() {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	// Countries in parallel, setups one after another within a country (one exit per country).
	perCountry := make([][]map[string]any, len(countries))
	var wg sync.WaitGroup
	for i, c := range countries {
		wg.Add(1)
		go func(i int, c country) {
			defer wg.Done()
			for _, s := range setups {
				perCountry[i] = append(perCountry[i], measure(pw, c, s))
			}
		}(i, c)
	}
	wg.Wait()
	var rows []map[string]any
	for _, r := range perCountry {
		rows = append(rows, r...)
	}
	out, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Println(string(out))
}
