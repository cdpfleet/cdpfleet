// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one load per public test page per setup)
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
	Name, Engine string
	Headless     bool
}

var setups = []setup{
	{"Brave, headless", "brave", true},
	{"Brave, headful", "brave", false},
	{"Google Chrome, headful", "chrome", false},
}

// Two made-up sites on different registrable domains, served by page routing: the browser
// treats them as two first-party sites, and no request leaves the browser.
var sites = map[string]string{"A": "https://site-a.example/", "B": "https://site-b.test/"}

const html = "<!doctype html><title>lab 83</title><p>A page with nothing on it but this probe.</p>"

// What a fingerprinting script reads, the same in every language: a canvas hash (drawn twice),
// an OfflineAudioContext hash, cores, memory, the plugin list, the screen and the window size.
const probe = `(async () => {
  const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
  const draw = () => {
    const c = document.createElement('canvas'); c.width = 240; c.height = 60;
    const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
    x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
    x.fillStyle = '#069'; x.fillText('cdpfleet fp 83 <@>', 2, 15);
    x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 83 <@>', 4, 17);
    return c.toDataURL();
  };
  const audio = async () => {
    try {
      const ctx = new OfflineAudioContext(1, 44100, 44100);
      const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
      const k = ctx.createDynamicsCompressor();
      k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
      o.connect(k); k.connect(ctx.destination); o.start(0);
      const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
      let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
      return fnv(s);
    } catch (e) { return 'error: ' + e.name; }
  };
  const c1 = draw(), c2 = draw();
  const plugins = Array.from(navigator.plugins).map((p) => p.name);
  return {
    site: location.hostname,
    canvas: fnv(c1), canvas_same_in_page: c1 === c2,
    audio: await audio(),
    cores: navigator.hardwareConcurrency, memory: navigator.deviceMemory ?? null,
    plugins: fnv(plugins.join(',')), plugin_names: plugins,
    screen: screen.width + 'x' + screen.height, window: outerWidth + 'x' + outerHeight,
  };
})()`

type testPage struct{ Key, URL, Ready, Read string }

// Each public test page's own verdict, read from its DOM; Ready is polled until the page has
// finished its checks. The same expressions run in every language.
var pages = []testPage{
	// A section CreepJS caught lying gets its hash marked with the "lies" class.
	{"creepjs", "https://abrahamjuliot.github.io/creepjs/",
		`/\d+% stealth/.test(document.body.innerText) && /privacy: \S/.test(document.body.innerText)`,
		`(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const m = /(\d+)% like headless[\s\S]*?(\d+)% headless:[\s\S]*?(\d+)% stealth/.exec(t); const privacy = /privacy: (\S+)/.exec(t); const noise = /rendering: (\d+% \w+ noise)/.exec(t); return { like_headless_pct: m ? +m[1] : null, headless_pct: m ? +m[2] : null, stealth_pct: m ? +m[3] : null, privacy: privacy ? privacy[1] : null, canvas_noise: noise ? noise[1] : null, lies: Array.from(document.querySelectorAll('.lies')).map((e) => (e.previousElementSibling ? e.previousElementSibling.innerText.trim() : '?')) }; })()`},
	{"browserleaks", "https://browserleaks.com/canvas",
		`/Uniqueness\s+\d/.test(document.body.innerText)`,
		`(() => { const t = document.body.innerText; const sig = /Signature\s+([0-9A-Fa-f]{32})/.exec(t); const u = /Uniqueness\s+([^\n]+)/.exec(t); return { signature: sig ? sig[1] : null, uniqueness: u ? u[1].trim() : null }; })()`},
	{"sannysoft", "https://bot.sannysoft.com/",
		`document.querySelectorAll('td.passed, td.failed').length > 10`,
		`(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()`},
	{"deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
		`document.body.innerText.indexOf('"isBot"') >= 0`,
		`(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('"isBot"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()`},
	// Only the bot verdict and the suspect score are read (the page also names proxy networks).
	{"fingerprint", "https://demo.fingerprint.com/playground",
		`/Suspect Score \d+/.test(document.body.innerText.replace(/\s+/g, ' '))`,
		`(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const bot = /\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\x20-\x7e]/g, '').replace(/\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()`},
}

type row struct {
	Setup    string           `json:"setup"`
	Engine   string           `json:"engine"`
	Headless bool             `json:"headless"`
	Visits   []map[string]any `json:"visits"`
	Pages    map[string]any   `json:"pages"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, headless bool) string {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "record": false, "headless": headless})
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			var session struct {
				WsURL string `json:"wsUrl"`
			}
			if err := json.Unmarshal(msg, &session); err != nil {
				log.Fatal(err)
			}
			return session.WsURL
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			log.Fatalf("launch %d %s", res.StatusCode, msg)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func connect(pw *playwright.Playwright, s setup) playwright.Browser {
	browser, err := pw.Chromium.Connect(launch(s.Engine, s.Headless), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	return browser
}

func siteContext(browser playwright.Browser) playwright.BrowserContext {
	context, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	for _, url := range sites {
		if err := context.Route(url+"**", func(route playwright.Route) {
			route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html"), Body: html})
		}); err != nil {
			log.Fatal(err)
		}
	}
	return context
}

// One visit = a new tab in the given context, the probe, the tab closed.
func visit(context playwright.BrowserContext, label string, launchNo, contextNo int, site string) map[string]any {
	page, err := context.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	defer page.Close()
	if _, err := page.Goto(sites[site]); err != nil {
		log.Fatal(err)
	}
	v, err := page.Evaluate(probe)
	if err != nil {
		log.Fatal(err)
	}
	out := v.(map[string]any)
	out["visit"], out["launch"], out["context"] = label, launchNo, contextNo
	return out
}

func errorOf(err error) map[string]any {
	msg := strings.SplitN(err.Error(), "\n", 2)[0]
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return map[string]any{"error": msg}
}

func verdict(page playwright.Page, p testPage) any {
	// A proxy tunnel drop is tried once more; a page that loaded is never reloaded.
	if _, err := page.Goto(p.URL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		if _, err := page.Goto(p.URL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
			return errorOf(err)
		}
	}
	if _, err := page.WaitForFunction(p.Ready, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(45000)}); err != nil {
		return errorOf(err)
	}
	v, err := page.Evaluate(p.Read)
	if err != nil {
		return errorOf(err)
	}
	return v
}

func run(pw *playwright.Playwright, s setup) row {
	r := row{Setup: s.Name, Engine: s.Engine, Headless: s.Headless, Pages: map[string]any{}}
	// Launch 1: site A, site B, site A again in a new tab, then site A in a second context.
	browser := connect(pw, s)
	one := siteContext(browser)
	r.Visits = append(r.Visits, visit(one, "site A", 1, 1, "A"), visit(one, "site B", 1, 1, "B"), visit(one, "site A again", 1, 1, "A"))
	r.Visits = append(r.Visits, visit(siteContext(browser), "site A, second context", 1, 2, "A"))
	browser.Close()
	// Launch 2: sites A and B again, then each public test page once, in a fresh context.
	browser = connect(pw, s)
	defer browser.Close()
	three := siteContext(browser)
	r.Visits = append(r.Visits, visit(three, "site A, next launch", 2, 1, "A"), visit(three, "site B, next launch", 2, 1, "B"))
	ctx, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	page, err := ctx.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range pages {
		r.Pages[p.Key] = verdict(page, p)
	}
	return r
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	out := map[string][]row{"setups": {}}
	for _, s := range setups {
		out["setups"] = append(out["setups"], run(pw, s))
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
