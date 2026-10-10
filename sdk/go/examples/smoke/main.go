// SDK smoke test on production: launch → connect → page → live link → keep_alive reconnect → close.
// env: CDPFLEET_API_KEY (a dedicated plan for keep_alive), PROXY_URL, PLAYWRIGHT_DRIVER_PATH
// (playwright-core 1.60 driver). Prints one JSON line.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	cdpfleet "github.com/cdpfleet/cdpfleet/sdk/go"
	"github.com/playwright-community/playwright-go"
)

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func main() {
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	fleet := must(cdpfleet.New())
	proxy := os.Getenv("PROXY_URL")
	var out struct {
		Title             string `json:"title"`
		LiveProtocol      any    `json:"live_protocol"`
		KeepAlivePageKept bool   `json:"keep_alive_page_kept"`
		Ended             bool   `json:"ended"`
		UsageOK           bool   `json:"usage_ok"`
	}

	s1 := must(fleet.Launch("chromium", cdpfleet.Options{"proxy": proxy, "headless": true, "live_view": true}))
	b1 := must(s1.Connect(pw.Chromium))
	p1 := must(b1.NewPage())
	must(p1.Goto("https://httpbin.org/html"))
	out.Title = must(p1.Locator("h1").TextContent())
	out.LiveProtocol = must(s1.LiveLink("view"))["protocol"]
	must(0, s1.Close())

	s2 := must(fleet.Launch("chromium", cdpfleet.Options{"proxy": proxy, "headless": true, "keep_alive": true, "inactivity_timeout": "2m"}))
	b2 := must(s2.Connect(pw.Chromium))
	p2 := must(must(b2.NewContext()).NewPage())
	must(p2.Goto("https://httpbin.org/html"))
	must(0, b2.Close())
	b2 = must(s2.Connect(pw.Chromium))
	kept := false
	for _, c := range b2.Contexts() {
		for _, p := range c.Pages() {
			kept = kept || strings.Contains(p.URL(), "httpbin")
		}
	}
	out.KeepAlivePageKept = kept
	must(0, s2.Close())
	threads := must(json.Marshal(must(fleet.Threads())))
	out.Ended = !strings.Contains(string(threads), s2.ID)
	_, isNumber := must(fleet.Usage())["used_thread_seconds"].(float64)
	out.UsageOK = isNumber
	fmt.Println(string(must(json.Marshal(out))))
}
