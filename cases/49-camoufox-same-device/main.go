// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_US
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

const site = "https://httpbin.org/html"

const fingerprint = `() => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const draw = () => {
    const c = document.createElement('canvas');
    c.width = 240; c.height = 60;
    const ctx = c.getContext('2d');
    ctx.font = '18px Arial'; ctx.fillStyle = '#f60'; ctx.fillRect(10, 10, 100, 30); ctx.fillStyle = '#069'; ctx.fillText('cdpfleet device 49', 4, 40);
    return c.toDataURL();
  };
  return {
    user_agent: navigator.userAgent, platform: navigator.platform, cores: navigator.hardwareConcurrency,
    screen: screen.width + 'x' + screen.height, webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    canvas: draw(), canvas_again: draw(), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    login_cookie: document.cookie.includes('session=alice-7f3'), login_storage: localStorage.getItem('user') === 'alice',
  };
}`

func shortHash(text string, n int) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:n]
}

func api(method, path string) (*http.Response, error) {
	req, _ := http.NewRequest(method, "https://cdpfleet.com"+path, nil)
	req.Header.Set("x-api-key", key)
	return http.DefaultClient.Do(req)
}

// The node saves the profile after the session's browser has exited: wait for the new version.
func waitForVersion(name string, version int) error {
	for i := 0; i < 60; i++ {
		res, err := api("GET", "/v1/me/profiles")
		if err == nil {
			var body struct {
				Profiles []struct {
					Name    string `json:"name"`
					Version int    `json:"version"`
					InUse   bool   `json:"in_use"`
				} `json:"profiles"`
			}
			json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			for _, p := range body.Profiles {
				if p.Name == name && p.Version >= version && !p.InUse {
					return nil
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("profile %s never reached version %d", name, version)
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/camoufox/session", bytes.NewReader(body))
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
			return nil, fmt.Errorf("launch %d %s", res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// Proxies drop a tunnel now and then: up to two retries.
func goTo(page playwright.Page, url string) error {
	for attempt := 1; ; attempt++ {
		_, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil || attempt == 3 {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}

type fp struct {
	UserAgent     string  `json:"user_agent"`
	Platform      string  `json:"platform"`
	Cores         int     `json:"cores"`
	Screen        string  `json:"screen"`
	WebGLRenderer *string `json:"webgl_renderer"`
	Canvas        string  `json:"canvas"`
	CanvasAgain   string  `json:"canvas_again"`
	Timezone      string  `json:"timezone"`
	LoginCookie   bool    `json:"login_cookie"`
	LoginStorage  bool    `json:"login_storage"`
}

// What a fingerprinting script reads, plus the "login" kept in a cookie and localStorage.
func visit(page playwright.Page, logIn bool) (fp, error) {
	var out fp
	if err := goTo(page, site); err != nil {
		return out, err
	}
	if logIn {
		if _, err := page.Evaluate("() => { document.cookie = 'session=alice-7f3; max-age=604800; path=/'; localStorage.setItem('user', 'alice'); }"); err != nil {
			return out, err
		}
	}
	raw, err := page.Evaluate(fingerprint)
	if err != nil {
		return out, err
	}
	b, _ := json.Marshal(raw)
	return out, json.Unmarshal(b, &out)
}

func session(pw *playwright.Playwright, opts map[string]any, logIn bool) (fp, error) {
	s, err := launch(opts)
	if err != nil {
		return fp{}, err
	}
	browser, err := pw.Firefox.Connect(s["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fp{}, err
	}
	defer browser.Close()
	// With a profile, its context is browser.Contexts()[0]; without one, a fresh context.
	var context playwright.BrowserContext
	if _, ok := opts["profile"]; ok {
		context = browser.Contexts()[0]
	} else if context, err = browser.NewContext(); err != nil {
		return fp{}, err
	}
	var page playwright.Page
	if pages := context.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = context.NewPage(); err != nil {
		return fp{}, err
	}
	return visit(page, logIn)
}

// Field order matches the other languages' output.
type row struct {
	Run                 int     `json:"run"`
	Device              string  `json:"device"`
	UserAgent           string  `json:"user_agent"`
	Cores               int     `json:"cores"`
	Screen              string  `json:"screen"`
	WebGLRenderer       *string `json:"webgl_renderer"`
	Canvas              string  `json:"canvas"`
	CanvasSameInSession bool    `json:"canvas_same_in_session"`
	Timezone            string  `json:"timezone"`
	LoginCookie         bool    `json:"login_cookie"`
	LoginStorage        bool    `json:"login_storage"`
}

// The device's hardware-like values (user agent, platform, cores, screen, GPU); the canvas is
// reported on its own. Compact JSON like JavaScript's JSON.stringify, so hashes match.
func pick(run int, f fp) row {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode([]any{f.UserAgent, f.Platform, f.Cores, f.Screen, f.WebGLRenderer})
	return row{run, shortHash(strings.TrimSpace(buf.String()), 10), f.UserAgent, f.Cores, f.Screen, f.WebGLRenderer,
		shortHash(f.Canvas, 12), f.Canvas == f.CanvasAgain, f.Timezone, f.LoginCookie, f.LoginStorage}
}

func run() error {
	rnd := make([]byte, 4)
	rand.Read(rnd)
	name := "case49-go-" + hex.EncodeToString(rnd) // one profile per run
	defer api("DELETE", "/v1/me/profiles/"+name)
	// One country for every launch: Camoufox matches timezone and locale to the exit IP.
	base := func() map[string]any {
		return map[string]any{"proxy": os.Getenv("PROXY_URL_US"), "headless": true, "os": "windows", "record": false}
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	// Four launches with one profile: the first creates it (device + login), the rest restore it.
	withProfile := []row{}
	for i := 1; i <= 4; i++ {
		opts := base()
		opts["profile"] = name
		f, err := session(pw, opts, i == 1)
		if err != nil {
			return err
		}
		withProfile = append(withProfile, pick(i, f))
		if err := waitForVersion(name, i); err != nil {
			return err
		}
	}
	// Two launches without a profile, for contrast.
	without := []row{}
	for i := 1; i <= 2; i++ {
		f, err := session(pw, base(), false)
		if err != nil {
			return err
		}
		without = append(without, pick(i, f))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"with_profile": withProfile, "without_profile": without}); err != nil {
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
