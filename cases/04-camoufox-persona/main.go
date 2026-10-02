// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (any exit), PROXY_URL_DE (an exit in Germany)
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

// A German Windows desktop: every value below is part of one consistent story.
func persona(proxy string) map[string]any {
	return map[string]any{
		"proxy":        proxy,
		"headless":     true,
		"os":           "windows",
		"locale":       "de-DE",
		"screen":       map[string]int{"minWidth": 1920, "maxWidth": 1920, "minHeight": 1080, "maxHeight": 1080},
		"window":       []int{1600, 900},
		"humanize":     true,
		"block_webrtc": true,
		"geoip":        true, // timezone and geolocation follow the proxy's exit IP
	}
}

const pageSignals = `() => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  return {
    platform: navigator.platform,
    languages: navigator.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    screen: screen.width + 'x' + screen.height,
    window: outerWidth + 'x' + outerHeight,
    hardware_concurrency: navigator.hardwareConcurrency,
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    webrtc: typeof RTCPeerConnection !== 'undefined',
  };
}`

func run(pw *playwright.Playwright, proxy string) map[string]any {
	session, err := launch("camoufox", persona(proxy))
	if err != nil {
		log.Fatal(err)
	}
	browser, err := pw.Firefox.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		log.Fatal(err)
	}
	defer browser.Close()
	page, _ := browser.NewPage()
	res, err := page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	var fp struct {
		UserAgent string `json:"user_agent"`
		TLS       struct {
			JA4 string `json:"ja4"`
		} `json:"tls"`
		HTTP2 struct {
			SentFrames []struct {
				FrameType string   `json:"frame_type"`
				Headers   []string `json:"headers"`
			} `json:"sent_frames"`
		} `json:"http2"`
	}
	res.JSON(&fp)
	seen, err := page.Evaluate(pageSignals)
	if err != nil {
		log.Fatal(err)
	}
	// Where the proxy exits, as a website would look it up.
	geoRes, err := page.Goto("http://ip-api.com/json/?fields=country,timezone", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		log.Fatal(err)
	}
	var geo struct{ Country, Timezone string }
	geoRes.JSON(&geo)
	out := seen.(map[string]any)
	out["exit_country"], out["exit_timezone"], out["user_agent"], out["ja4"] = geo.Country, geo.Timezone, fp.UserAgent, fp.TLS.JA4
	out["accept_language"] = nil
	for _, f := range fp.HTTP2.SentFrames {
		for _, h := range f.Headers {
			if f.FrameType == "HEADERS" && strings.HasPrefix(h, "accept-language: ") {
				out["accept_language"] = h[17:]
			}
		}
	}
	return out
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	out, _ := json.MarshalIndent(map[string]any{
		"random exit": run(pw, os.Getenv("PROXY_URL")),
		"German exit": run(pw, os.Getenv("PROXY_URL_DE")),
	}, "", "  ")
	fmt.Println(string(out))
}
