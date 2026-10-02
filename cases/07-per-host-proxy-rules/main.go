// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs, 3 or more)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

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

var ipPattern = regexp.MustCompile(`\d{1,3}(\.\d{1,3}){3}`)

func hostOf(u string) string { p, _ := url.Parse(u); return p.Hostname() }

// Each context has its own connection pool, so each reading is a fresh connection.
// Proxies drop a connection now and then: one retry.
func exitIPVia(browser playwright.Browser, u string) string {
	for attempt := 1; ; attempt++ {
		context, err := browser.NewContext()
		if err != nil {
			log.Fatal(err)
		}
		page, _ := context.NewPage()
		res, err := page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
		var text string
		if err == nil {
			text, err = res.Text()
		}
		context.Close()
		if err == nil {
			if ip := ipPattern.FindString(text); ip != "" {
				return ip
			}
			return "(no IP in " + u + ")"
		}
		if attempt == 2 {
			return "(failed: " + strings.SplitN(err.Error(), "\n", 2)[0] + ")"
		}
	}
}

type reading struct {
	URL    string `json:"url"`
	ExitIP string `json:"exit_ip"`
}

func main() {
	socks := strings.Split(os.Getenv("SOCKS_PROXIES"), ",")
	session, err := launch("chromium", map[string]any{
		"proxy": os.Getenv("PROXY_URL"), // everything not matched below
		"proxy_rules": []map[string]any{
			{"hosts": []string{"*.ident.me", "ident.me"}, "proxy": socks[0]},
			{"hosts": []string{"httpbin.org", "*.httpbin.org"}, "proxy": []string{socks[1], socks[2]}}, // round-robin
			// IP-lookup services always use the default proxy, so this rule is ignored on purpose.
			{"hosts": []string{"api.ipify.org"}, "proxy": socks[0]},
		},
		"headless": true,
	})
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
	defer browser.Close()

	readings := []reading{}
	for _, u := range []string{"https://v4.ident.me/", "https://httpbin.org/ip", "https://httpbin.org/ip", "https://httpbin.org/ip",
		"https://api.ipify.org/", "https://www.cloudflare.com/cdn-cgi/trace"} {
		readings = append(readings, reading{u, exitIPVia(browser, u)})
	}
	out, _ := json.MarshalIndent(map[string]any{
		"rules": map[string]any{
			"*.ident.me":        hostOf(socks[0]),
			"httpbin.org":       []string{hostOf(socks[1]), hostOf(socks[2])},
			"api.ipify.org":     hostOf(socks[0]) + " (ignored: IP-lookup host)",
			"(everything else)": "residential PROXY_URL",
		},
		"readings": readings,
	}, "", "  ")
	fmt.Println(string(out))
}
