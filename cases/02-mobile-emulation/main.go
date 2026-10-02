// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

const pageSignals = `() => ({
  viewport: innerWidth + 'x' + innerHeight,
  screen: screen.width + 'x' + screen.height,
  device_pixel_ratio: devicePixelRatio,
  max_touch_points: navigator.maxTouchPoints,
  coarse_pointer: matchMedia('(pointer: coarse)').matches,
  platform: navigator.platform,
  ua_data_mobile: navigator.userAgentData ? navigator.userAgentData.mobile : null,
  ua_data_platform: navigator.userAgentData ? navigator.userAgentData.platform : null,
})`

type peet struct {
	UserAgent string `json:"user_agent"`
	TLS       struct {
		JA4 string `json:"ja4"`
	} `json:"tls"`
	HTTP2 struct {
		AkamaiFingerprintHash string `json:"akamai_fingerprint_hash"`
		SentFrames            []struct {
			FrameType string   `json:"frame_type"`
			Headers   []string `json:"headers"`
		} `json:"sent_frames"`
	} `json:"http2"`
}

// What a page can see about the device, plus what the network sees (tls.peet.ws).
func inspect(context playwright.BrowserContext) map[string]any {
	page, _ := context.NewPage()
	defer page.Close()
	res, err := page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	var fp peet
	if err := res.JSON(&fp); err != nil {
		log.Fatal(err)
	}
	js, err := page.Evaluate(pageSignals)
	if err != nil {
		log.Fatal(err)
	}
	out := js.(map[string]any)
	header := func(name string) any {
		for _, f := range fp.HTTP2.SentFrames {
			for _, h := range f.Headers {
				if f.FrameType == "HEADERS" && strings.HasPrefix(h, name+": ") {
					return h[len(name)+2:]
				}
			}
		}
		return nil
	}
	out["user_agent"] = fp.UserAgent
	out["sec_ch_ua_mobile"] = header("sec-ch-ua-mobile")
	out["sec_ch_ua_platform"] = header("sec-ch-ua-platform")
	out["ja4"] = fp.TLS.JA4
	out["akamai_h2_hash"] = fp.HTTP2.AkamaiFingerprintHash
	return out
}

// Playwright's device descriptors as new-context options.
func device(d *playwright.DeviceDescriptor) playwright.BrowserNewContextOptions {
	return playwright.BrowserNewContextOptions{
		UserAgent: playwright.String(d.UserAgent), Viewport: d.Viewport, Screen: d.Screen,
		DeviceScaleFactor: playwright.Float(d.DeviceScaleFactor), IsMobile: playwright.Bool(d.IsMobile), HasTouch: playwright.Bool(d.HasTouch),
	}
}

func main() {
	session, err := launch("chrome", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
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

	desktop, _ := browser.NewContext()
	iphone, _ := browser.NewContext(device(pw.Devices["iPhone 15 Pro"]))
	pixel, _ := browser.NewContext(device(pw.Devices["Pixel 7"]))
	out, _ := json.MarshalIndent(map[string]any{
		"desktop":       inspect(desktop),
		"iPhone 15 Pro": inspect(iphone),
		"Pixel 7":       inspect(pixel),
	}, "", "  ")
	fmt.Println(string(out))
}
