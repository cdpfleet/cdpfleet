// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
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
	"sort"
	"strconv"
	"time"

	"github.com/playwright-community/playwright-go"
)

var (
	key   = os.Getenv("CDPFLEET_API_KEY")
	proxy = os.Getenv("PROXY_URL")
)

const site = "https://www.httpbin.org"

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var out map[string]any
			return out, json.Unmarshal(data, &out)
		}
		if res.StatusCode != 503 || attempt == 5 {
			return nil, fmt.Errorf("launch %d %s", res.StatusCode, data)
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func open(pw *playwright.Playwright, engine string) playwright.Browser {
	session, err := launch(engine, map[string]any{"proxy": proxy, "headless": true, "record": false})
	if err != nil {
		log.Fatal(err)
	}
	types := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}
	browser, err := types[engine].Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	return browser
}

// httpbin answers 502/503 now and then: retry a page that isn't JSON.
func getJSON(page playwright.Page, url string) map[string]any {
	for attempt := 1; ; attempt++ {
		if _, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(45000)}); err != nil {
			log.Fatal(err)
		}
		text, _ := page.TextContent("body")
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err == nil {
			return out
		} else if attempt == 4 {
			log.Fatal(err)
		}
		time.Sleep(3 * time.Second)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func names(cookies []playwright.Cookie) []string {
	out := []string{}
	for _, c := range cookies {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

func find(cookies []playwright.Cookie, name string) playwright.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	log.Fatalf("no cookie %s", name)
	return playwright.Cookie{}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

type pslRow struct {
	Engine         string   `json:"engine"`
	Host           string   `json:"host"`
	AddCookies     bool     `json:"add_cookies"`
	SetCookie      bool     `json:"set_cookie"`
	DocumentCookie bool     `json:"document_cookie"`
	ServerSees     []string `json:"server_sees"`
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()

	// Part 1: inspect, edit, clear and carry the jar (Chromium).
	jar := map[string]any{}
	var saved []byte
	func() {
		browser := open(pw, "chromium")
		defer browser.Close()
		context := must(browser.NewContext())
		page := must(context.NewPage())
		// The site sets two cookies (no expiry: session cookies); another site sets one more.
		getJSON(page, site+"/cookies/set?session_token=abc123&theme=dark")
		must(0, context.AddCookies([]playwright.OptionalCookie{{Name: "other_site", Value: "1", URL: playwright.String("https://example.com")}}))
		jar["all_cookies"] = names(must(context.Cookies()))
		mine := must(context.Cookies(site)) // only the cookies that would be sent to this URL
		jar["for_site"] = names(mine)
		jar["session_cookie_expires"] = int(find(mine, "session_token").Expires) // -1 = session cookie

		// Edit: AddCookies with the same name, domain and path replaces the cookie.
		token := find(mine, "session_token").ToOptionalCookie()
		token.Expires = playwright.Float(float64(time.Now().Unix() + 7*86400))
		theme := find(mine, "theme").ToOptionalCookie()
		theme.Value = "light"
		must(0, context.AddCookies([]playwright.OptionalCookie{token, theme}))
		expires := find(must(context.Cookies(site)), "session_token").Expires
		jar["token_expires_in_days"] = int(math.Round((expires - float64(time.Now().Unix())) / 86400))
		jar["server_sees_after_edit"] = getJSON(page, site+"/cookies")["cookies"]

		// Clear one cookie by name (other filters: domain, path).
		must(0, context.ClearCookies(playwright.BrowserContextClearCookiesOptions{Name: "theme"}))
		jar["after_clear_by_name"] = names(must(context.Cookies()))

		// Carry the login as plain JSON: just the site's cookies.
		saved = must(json.Marshal(must(context.Cookies(site))))
		jar["saved_json_bytes"] = len(saved)
	}()
	func() {
		browser := open(pw, "chromium") // a new session on another browser
		defer browser.Close()
		context := must(browser.NewContext())
		jar["new_session_before"] = names(must(context.Cookies()))
		var loaded []playwright.Cookie
		must(0, json.Unmarshal(saved, &loaded))
		cookies := []playwright.OptionalCookie{}
		for _, c := range loaded {
			cookies = append(cookies, c.ToOptionalCookie())
		}
		must(0, context.AddCookies(cookies))
		page := must(context.NewPage())
		jar["new_session_server_sees"] = getJSON(page, site+"/cookies")["cookies"]
	}()

	// Part 2: the same three ways to set a cookie, on three hosts, in three engines.
	// httpbin.org and postman-echo.com are on the Public Suffix List; www.httpbin.org isn't.
	psl := []pslRow{}
	for _, engine := range []string{"chromium", "firefox", "webkit"} {
		func() {
			browser := open(pw, engine)
			defer browser.Close()
			for _, host := range []string{"httpbin.org", "www.httpbin.org", "postman-echo.com"} {
				context := must(browser.NewContext())
				page := must(context.NewPage())
				url := "https://" + host
				must(0, context.AddCookies([]playwright.OptionalCookie{{Name: "via_add_cookies", Value: "1", URL: playwright.String(url)}}))
				getJSON(page, url+"/cookies/set?via_set_cookie=1") // Set-Cookie, then a redirect to /cookies
				must(page.Evaluate("() => { document.cookie = 'via_document_cookie=1; path=/; max-age=3600'; }"))
				kept := names(must(context.Cookies(url)))
				server := []string{}
				for name := range getJSON(page, url+"/cookies")["cookies"].(map[string]any) {
					server = append(server, name)
				}
				sort.Strings(server)
				psl = append(psl, pslRow{engine, host, contains(kept, "via_add_cookies"), contains(kept, "via_set_cookie"), contains(kept, "via_document_cookie"), server})
				context.Close()
			}
		}()
	}

	out, _ := json.MarshalIndent(map[string]any{"jar": jar, "psl": psl}, "", "  ")
	fmt.Println(string(out))
}
