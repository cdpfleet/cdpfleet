// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (one session at a time; public test pages only; nothing is solved or retried to get a result)
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// One sticky residential exit for the whole run, shared by every setup: a difference between rows is the browser
// and how it was driven, not the IP.
var proxy = func() string {
	b := make([]byte, 3)
	rand.Read(b)
	return "cdpfleet-resi-country-us-session-c81" + hex.EncodeToString(b) + "-lifetime-30"
}()

const repeats = 3

type setup struct {
	Key, Name, Engine, Type string
	Body                    map[string]any
}

// Camoufox's humanize option: false = the cursor jumps straight to the target, true (the default) = a curved path of
// up to 1.5 s, a number = a path of up to that many seconds. Chrome is the reference: plain Playwright clicks.
var setups = []setup{
	{"cf_off", "Camoufox, humanize: false", "camoufox", "firefox", map[string]any{"headless": true, "humanize": false}},
	{"cf_on", "Camoufox, humanize: true (default)", "camoufox", "firefox", map[string]any{"headless": true, "humanize": true}},
	{"cf_slow", "Camoufox, humanize: 3", "camoufox", "firefox", map[string]any{"headless": true, "humanize": 3}},
	{"chrome", "Google Chrome, headful", "chrome", "chromium", map[string]any{"headless": false}},
}

// The same actions in every cell; only the typing speed differs between the two form runs of a session.
var typings = []struct {
	Name  string
	Delay float64 // ms between keys
}{{"fast", 0}, {"paced", 120}}

const email = "lab81@example.com"
const password = "lab-81!pass" // the demo form wants 8+ characters with a special character

// deviceandbrowserinfo.com's behavioural bot test: a demo login form that records clicks, keystrokes and timing
// and, on submit, posts them with a fingerprint to its own endpoint, which answers human or bot with its flags.
const formURL = "https://deviceandbrowserinfo.com/are_you_a_bot_interactions"

// Google's public reCAPTCHA v3 demo prints its server's verify response (with the score); its "Try again" link reloads it.
const recaptchaURL = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php"

// The same expressions run in every language.
// Count mousemove events on the page, so the table shows what humanize changed.
const countMovesJS = `(() => { window.__lab81 = 0; document.addEventListener('mousemove', () => { window.__lab81++; }, true); return true; })()`

// The verdict the page shows after submit: isBot and every detail flag that is true.
const verdictJS = `(() => {
  const e = document.getElementById('jsonResult');
  const t = e ? e.innerText : '';
  if (t.length < 10) return null;
  const r = JSON.parse(t);
  const d = r.details || {};
  return { is_bot: r.isBot === true, flags: Object.keys(d).filter((k) => d[k] === true), moves: window.__lab81 };
})()`

// What the page itself measured about the interaction: the "interactions" part of the body it posted, which is
// embedded as a JSON string and passed to this function.
const measuredJS = `(body) => {
  const i = JSON.parse(body).interactions || {};
  const s = i.emailTimingStats || {};
  return {
    time_to_fill_ms: Math.round(i.timeToFill),
    exact_center_clicks: [i.hasClickedEmailFieldExactCenter, i.hasClickedPasswordFieldExactCenter, i.hasClickedSubmitExactCenter].filter(Boolean).length,
    untrusted_event: i.hasUntrustedEvent === true,
    cdp_mouse_leak: i.cdpMouseLeak === true,
    key_gap_median_ms: typeof s.medianTimeBetweenKeys === 'number' ? Math.round(s.medianTimeBetweenKeys) : null,
  };
}`

const scoreJS = `(() => { if (window.__lab81rc) return null; const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()`

type measured struct {
	TimeToFillMs      int  `json:"time_to_fill_ms"`
	ExactCenterClicks int  `json:"exact_center_clicks"`
	UntrustedEvent    bool `json:"untrusted_event"`
	CdpMouseLeak      bool `json:"cdp_mouse_leak"`
	KeyGapMedianMs    *int `json:"key_gap_median_ms"`
}
type formRun struct {
	Setup         string    `json:"setup"`
	Key           string    `json:"key"`
	Repeat        int       `json:"repeat"`
	Typing        string    `json:"typing"`
	Verdict       string    `json:"verdict"`
	Flags         []string  `json:"flags"`
	Moves         *int      `json:"moves"`
	ActionSeconds *float64  `json:"action_seconds"`
	Measured      *measured `json:"measured"`
}
type scoreRun struct {
	Setup  string   `json:"setup"`
	Key    string   `json:"key"`
	Repeat int      `json:"repeat"`
	Before *float64 `json:"before"`
	After  *float64 `json:"after"`
}

var pw *playwright.Playwright

func launch(s setup) (playwright.Browser, error) {
	body := map[string]any{"proxy": proxy, "record": false}
	for k, v := range s.Body {
		body[k] = v
	}
	payload, _ := json.Marshal(body)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+s.Engine+"/session", bytes.NewReader(payload))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session struct{ WsURL string }
			json.Unmarshal(msg, &session)
			bt := pw.Chromium
			if s.Type == "firefox" {
				bt = pw.Firefox
			}
			return bt.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			return nil, errors.New("launch " + strconv.Itoa(res.StatusCode) + " " + string(msg))
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// A load that fails on the network (proxy timeout) is tried once more; the page's answer is never retried.
func open(tab playwright.Page, url string) bool {
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := tab.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(30000)})
		if err == nil {
			return true
		}
	}
	return false
}

// decode copies what evaluate returned into a typed value.
func decode(v any, into any) bool {
	if v == nil {
		return false
	}
	raw, err := json.Marshal(v)
	return err == nil && json.Unmarshal(raw, into) == nil
}

func score(tab playwright.Page) *float64 {
	v, err := tab.Evaluate(scoreJS)
	if err != nil || v == nil {
		return nil
	}
	var f float64
	if !decode(v, &f) {
		return nil
	}
	return &f
}

// One form run: click the email field, type, click the password field, type, click Login; read the verdict.
func form(browser playwright.Browser, run formRun, delay float64) formRun {
	run.Verdict, run.Flags = "error", []string{}
	ctx, err := browser.NewContext() // a fresh context: every run is a first visit
	if err != nil {
		return run
	}
	defer ctx.Close()
	tab, err := ctx.NewPage()
	if err != nil {
		return run
	}
	var mu sync.Mutex
	posted := ""
	tab.OnRequest(func(req playwright.Request) {
		if strings.Contains(req.URL(), "/fingerprint_bot_test") {
			if body, err := req.PostData(); err == nil {
				mu.Lock()
				posted = body
				mu.Unlock()
			}
		}
	})
	if !open(tab, formURL) {
		return run
	}
	time.Sleep(time.Second)
	if _, err := tab.Evaluate(countMovesJS); err != nil {
		return run
	}
	t0 := time.Now()
	steps := []func() error{
		func() error { return tab.Click("#email") },
		func() error { return tab.Keyboard().Type(email, playwright.KeyboardTypeOptions{Delay: playwright.Float(delay)}) },
		func() error { return tab.Click("#password") },
		func() error { return tab.Keyboard().Type(password, playwright.KeyboardTypeOptions{Delay: playwright.Float(delay)}) },
		func() error { return tab.Click("#loginForm button[type=submit]") },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return run
		}
	}
	secs := math.Round(time.Since(t0).Seconds()*10) / 10
	run.ActionSeconds = &secs
	var v struct {
		IsBot bool     `json:"is_bot"`
		Flags []string `json:"flags"`
		Moves *int     `json:"moves"`
	}
	got := false
	for i := 0; i < 60 && !got; i++ {
		time.Sleep(500 * time.Millisecond)
		if r, err := tab.Evaluate(verdictJS); err == nil {
			got = decode(r, &v)
		}
	}
	if !got {
		run.Verdict = "no verdict"
		return run
	}
	run.Verdict, run.Flags, run.Moves = "human", v.Flags, v.Moves
	if v.IsBot {
		run.Verdict = "bot"
	}
	mu.Lock()
	body := posted
	mu.Unlock()
	if body != "" {
		arg, _ := json.Marshal(body)
		if r, err := tab.Evaluate("(" + measuredJS + ")(" + string(arg) + ")"); err == nil {
			var m measured
			if decode(r, &m) {
				run.Measured = &m
			}
		}
	}
	return run
}

// Google's v3 demo: wait for the score it asks for on load, move the cursor and click "Try again", read the new score.
func recaptcha(browser playwright.Browser, run scoreRun) scoreRun {
	ctx, err := browser.NewContext()
	if err != nil {
		return run
	}
	defer ctx.Close()
	tab, err := ctx.NewPage()
	if err != nil || !open(tab, recaptchaURL) {
		return run
	}
	for i := 0; i < 40 && run.Before == nil; i++ {
		time.Sleep(500 * time.Millisecond)
		run.Before = score(tab)
	}
	// Marks this document, so the score is read from the reloaded one.
	if _, err := tab.Evaluate("window.__lab81rc = 1"); err != nil {
		return run
	}
	if tab.Mouse().Move(300, 300) != nil || tab.Mouse().Move(600, 200) != nil || tab.Click(`a[href="/recaptcha-v3-request-scores.php"]`) != nil {
		return run
	}
	for i := 0; i < 40 && run.After == nil; i++ {
		time.Sleep(500 * time.Millisecond)
		run.After = score(tab)
	}
	return run
}

func main() {
	var err error
	pw, err = playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	runs := []formRun{}
	scores := []scoreRun{}
	// Serial: one session at a time; the repeats are interleaved so a slow minute doesn't land on one setup.
	for repeat := 1; repeat <= repeats; repeat++ {
		for _, s := range setups {
			browser, err := launch(s)
			for _, t := range typings {
				run := formRun{Setup: s.Name, Key: s.Key, Repeat: repeat, Typing: t.Name, Verdict: "error", Flags: []string{}}
				if err == nil {
					run = form(browser, run, t.Delay)
				}
				runs = append(runs, run)
			}
			sc := scoreRun{Setup: s.Name, Key: s.Key, Repeat: repeat}
			if err == nil {
				sc = recaptcha(browser, sc)
				browser.Close()
			}
			scores = append(scores, sc)
		}
	}
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "runs": runs, "recaptcha": scores}, "", "  ")
	fmt.Println(string(out))
}
