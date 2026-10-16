// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, SOCKS_PROXIES (comma-separated sticky socks5:// URLs: one exit IP each)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var (
	key     = os.Getenv("CDPFLEET_API_KEY")
	proxies = strings.Split(os.Getenv("SOCKS_PROXIES"), ",")
)

const challenge = "<title>Just a moment...</title><p>Checking your browser before accessing the site.</p>"

type ipState struct {
	served     int
	last       time.Time
	retryAt    time.Time
	challenged bool
}

// A stand-in for a rate-limited site, in front of real httpbin requests. Each request really goes
// out through the session's proxy; the limiter decides by the IP httpbin saw (its "origin"):
// 3 pages in a burst, then one page per 5 s (429 + Retry-After: 5), 6 pages per IP in total. An IP
// that comes back before its Retry-After, or is over its quota, gets a challenge page from then on.
func limiter() func(playwright.Route) {
	ips := map[string]*ipState{}
	return func(route playwright.Route) {
		now := time.Now() // when the request arrived
		real, err := route.Fetch(playwright.RouteFetchOptions{Timeout: playwright.Float(30000)})
		if err != nil { // the proxy hiccuped
			route.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(502), ContentType: playwright.String("text/plain"), Body: "Bad Gateway"})
			return
		}
		if real.Status() != 200 { // httpbin itself failed
			route.Fulfill(playwright.RouteFulfillOptions{Response: real})
			return
		}
		var body struct {
			Origin string `json:"origin"`
		}
		real.JSON(&body)
		s, ok := ips[body.Origin]
		if !ok {
			s = &ipState{}
			ips[body.Origin] = s
		}
		if now.Before(s.retryAt) || s.served >= 6 {
			s.challenged = true
		}
		if s.challenged {
			route.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(403), ContentType: playwright.String("text/html"), Body: challenge})
			return
		}
		if s.served >= 3 && now.Sub(s.last) < 5*time.Second {
			s.retryAt = now.Add(5 * time.Second)
			route.Fulfill(playwright.RouteFulfillOptions{Status: playwright.Int(429), Headers: map[string]string{"retry-after": "5"}, ContentType: playwright.String("text/plain"), Body: "Too Many Requests"})
			return
		}
		s.served++
		s.last = now
		route.Fulfill(playwright.RouteFulfillOptions{Response: real})
	}
}

var challengeText = regexp.MustCompile(`(?i)just a moment|captcha|access denied`)

// What the response means: the data, a rate limit (wait), or a block (this IP is done).
func classify(status int, text string) string {
	if status == 429 {
		return "rate_limited"
	}
	if status == 403 || status == 503 || challengeText.MatchString(text) {
		return "blocked"
	}
	if status == 200 {
		return "ok"
	}
	return "error"
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
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

// Live proxy swap on the session's router: new connections use the new proxy.
func swapProxy(wsURL, sessionID, proxy string) error {
	u, _ := url.Parse(wsURL)
	body, _ := json.Marshal(map[string]string{"session_id": sessionID, "proxy": proxy})
	req, _ := http.NewRequest("POST", "https://"+u.Host+"/admin/session/proxy", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("swap %d %s", res.StatusCode, b)
	}
	return nil
}

type pageRow struct {
	Page    int     `json:"page"`
	Outcome string  `json:"outcome"`
	ExitIP  *string `json:"exit_ip"`
	Trail   string  `json:"trail"`
}

type crawlResult struct {
	Client            string    `json:"client"`
	PagesFetched      int       `json:"pages_fetched"`
	Exits             []string  `json:"exits"`
	WaitedSeconds     int       `json:"waited_seconds"`
	ProxySwaps        int       `json:"proxy_swaps"`
	SessionCookieKept bool      `json:"session_cookie_kept"`
	Pages             []pageRow `json:"pages"`
}

func crawl(pw *playwright.Playwright, polite bool) (crawlResult, error) {
	out := crawlResult{Client: "naive", Exits: []string{}, Pages: []pageRow{}}
	first := proxies[0]
	if polite {
		out.Client, first = "polite", proxies[1]
	}
	session, err := launch(map[string]any{"proxy": first, "headless": "new", "record": false})
	if err != nil {
		return out, err
	}
	wsURL, sessionID := session["wsUrl"].(string), session["sessionId"].(string)
	browser, err := pw.Chromium.Connect(wsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()
	site := limiter()
	open := func(state *playwright.StorageState) (playwright.BrowserContext, playwright.Page, error) {
		opts := playwright.BrowserNewContextOptions{}
		if state != nil {
			opts.StorageState = state.ToOptionalStorageState()
		}
		context, err := browser.NewContext(opts)
		if err != nil {
			return nil, nil, err
		}
		if err := context.Route("https://httpbin.org/anything/**", site); err != nil {
			return nil, nil, err
		}
		page, err := context.NewPage()
		return context, page, err
	}
	context, page, err := open(nil)
	if err != nil {
		return out, err
	}
	// A logged-in state that must survive a proxy swap.
	if _, err := page.Goto("https://httpbin.org/cookies/set?session=alice-42", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return out, err
	}
	next := 2
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		pageURL := fmt.Sprintf("https://httpbin.org/anything/catalogue/page-%d", i+1)
		trail := []string{}
		row := pageRow{Page: i + 1, Outcome: "gave up"}
		for attempt := 1; attempt <= 6; attempt++ {
			res, err := page.Goto(pageURL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
			if err != nil {
				return out, err
			}
			text, err := page.TextContent("body")
			if err != nil {
				return out, err
			}
			kind := classify(res.Status(), text)
			trail = append(trail, strconv.Itoa(res.Status()))
			if kind == "ok" {
				var body struct {
					Origin string `json:"origin"`
				}
				json.Unmarshal([]byte(text), &body)
				row.Outcome, row.ExitIP = "ok", &body.Origin
				break
			}
			if kind == "rate_limited" {
				if !polite && attempt == 3 {
					break
				}
				seconds := 0
				if polite {
					// Honour Retry-After; without one, back off exponentially (1, 2, 4… s, capped).
					if seconds, err = strconv.Atoi(res.Headers()["retry-after"]); err != nil {
						seconds = int(math.Min(math.Pow(2, float64(attempt-1)), 30))
					}
					trail = append(trail, fmt.Sprintf("wait %ds", seconds))
				}
				out.WaitedSeconds += seconds
				time.Sleep(time.Duration(seconds) * time.Second)
				continue
			}
			if kind == "blocked" && polite && next < len(proxies) {
				// This exit is done: move the session to another proxy and continue in a new context
				// (its own connections) that keeps the cookies and storage.
				if err := swapProxy(wsURL, sessionID, proxies[next]); err != nil {
					return out, err
				}
				next++
				state, err := context.StorageState()
				if err != nil {
					return out, err
				}
				context.Close()
				if context, page, err = open(state); err != nil {
					return out, err
				}
				out.ProxySwaps++
				trail = append(trail, "swap proxy")
				continue
			}
			if kind == "error" {
				time.Sleep(time.Second) // a transient failure: try again
				continue
			}
			break
		}
		row.Trail = strings.Join(trail, " → ")
		out.Pages = append(out.Pages, row)
		if row.ExitIP != nil {
			out.PagesFetched++
			if !seen[*row.ExitIP] {
				seen[*row.ExitIP] = true
				out.Exits = append(out.Exits, *row.ExitIP)
			}
		}
	}
	if _, err := page.Goto("https://httpbin.org/cookies", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return out, err
	}
	text, err := page.TextContent("body")
	if err != nil {
		return out, err
	}
	var cookies struct {
		Cookies map[string]string `json:"cookies"`
	}
	json.Unmarshal([]byte(text), &cookies)
	out.SessionCookieKept = cookies.Cookies["session"] == "alice-42"
	return out, nil
}

func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	// Naive: retries a 429 straight away and has no plan for a block. Polite: waits as told and
	// moves to another exit when one is blocked.
	naive, err := crawl(pw, false)
	if err != nil {
		return fmt.Errorf("naive: %w", err)
	}
	polite, err := crawl(pw, true)
	if err != nil {
		return fmt.Errorf("polite: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]crawlResult{"naive": naive, "polite": polite})
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
