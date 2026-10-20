// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one page load per test page per setup)
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
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

type setup struct {
	Name, Engine, Family string
	Body                 map[string]any
}

var setups = []setup{
	{"Chromium, headless", "chromium", "chromium", map[string]any{"headless": "new"}},
	{"Google Chrome, headful", "chrome", "chromium", map[string]any{"headless": false}},
	{"Patchright, headful", "patchright", "chromium", map[string]any{"headless": false}},
	{"Camoufox, defaults", "camoufox", "firefox", map[string]any{"headless": true}},
	{"WebKit, headless", "webkit", "webkit", map[string]any{"headless": true}},
}

type testPage struct{ Key, URL, Ready, Read string }

// Each test page's own verdict, read from its DOM. The same expressions run in every language;
// Ready is polled until the page has finished its checks.
var pages = []testPage{
	{"sannysoft", "https://bot.sannysoft.com/",
		`document.querySelectorAll('td.passed, td.failed').length > 10`,
		`(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()`},
	{"creepjs", "https://abrahamjuliot.github.io/creepjs/",
		`/\d+% stealth/.test(document.body.innerText)`,
		`(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const m = /(\d+)% like headless[\s\S]*?(\d+)% headless:[\s\S]*?(\d+)% stealth/.exec(t); return m ? { like_headless_pct: +m[1], headless_pct: +m[2], stealth_pct: +m[3] } : { like_headless_pct: null, headless_pct: null, stealth_pct: null }; })()`},
	{"deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
		`document.body.innerText.indexOf('"isBot"') >= 0`,
		`(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('"isBot"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()`},
	// Only the bot verdict and the suspect score are read (the page also names proxy networks).
	{"fingerprint", "https://demo.fingerprint.com/playground",
		`/Suspect Score \d+/.test(document.body.innerText.replace(/\s+/g, ' '))`,
		`(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const bot = /\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\x20-\x7e]/g, '').replace(/\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()`},
}

type row struct {
	Setup                string `json:"setup"`
	Engine               string `json:"engine"`
	Sannysoft            any    `json:"sannysoft"`
	Creepjs              any    `json:"creepjs"`
	Deviceandbrowserinfo any    `json:"deviceandbrowserinfo"`
	Fingerprint          any    `json:"fingerprint"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, options map[string]any) (map[string]any, error) {
	payload := map[string]any{"proxy": os.Getenv("PROXY_URL"), "record": false}
	for k, v := range options {
		payload[k] = v
	}
	body, _ := json.Marshal(payload)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		if res.StatusCode == http.StatusOK {
			var session map[string]any
			err := json.NewDecoder(res.Body).Decode(&session)
			res.Body.Close()
			return session, err
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			return nil, fmt.Errorf("launch %d %s", res.StatusCode, msg)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func errorOf(err error) map[string]any {
	msg := strings.SplitN(err.Error(), "\n", 2)[0]
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return map[string]any{"error": msg}
}

func run(pw *playwright.Playwright, s setup) row {
	r := row{Setup: s.Name, Engine: s.Engine}
	session, err := launch(s.Engine, s.Body)
	if err != nil {
		log.Fatal(err)
	}
	types := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}
	browser, err := types[s.Family].Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()
	ctx, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	page, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	verdicts := []*any{&r.Sannysoft, &r.Creepjs, &r.Deviceandbrowserinfo, &r.Fingerprint}
	for i, p := range pages {
		*verdicts[i] = func() any {
			if _, err := page.Goto(p.URL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
				return errorOf(err)
			}
			if _, err := page.WaitForFunction(p.Ready, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(45000)}); err != nil {
				return errorOf(err)
			}
			v, err := page.Evaluate(p.Read)
			if err != nil {
				return errorOf(err)
			}
			return v
		}()
	}
	return r
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	results := []row{}
	for _, s := range setups {
		results = append(results, run(pw, s))
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
}
