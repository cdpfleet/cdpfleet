// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY
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
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

type setup struct {
	ID, Name, Engine, Type string
	Headless               any // false, true or "new"
}

// The same twelve setups as the real-site lab cases 70-77 (Rebrowser needs a 1.52 client, so it's left out).
var setups = []setup{
	{"chrome_headful", "Chrome, headful", "chrome", "chromium", false},
	{"chrome_headless", "Chrome, headless", "chrome", "chromium", "new"},
	{"edge", "Edge, headful", "edge", "chromium", false},
	{"brave", "Brave, headful", "brave", "chromium", false},
	{"opera", "Opera, headful", "opera", "chromium", false},
	{"yandex", "Yandex, headful", "yandex", "chromium", false},
	{"whale", "Whale, headful", "whale", "chromium", false},
	{"chromium_headless", "Chromium, headless", "chromium", "chromium", true},
	{"patchright", "Patchright, headful", "patchright", "chromium", false},
	{"firefox", "Firefox, headful", "firefox", "firefox", false},
	{"webkit", "WebKit, headful", "webkit", "webkit", false},
	{"camoufox", "Camoufox, headful", "camoufox", "firefox", false},
}

type fingerprint struct {
	Version       string  `json:"version"`
	Source        string  `json:"source"`
	HTTPVersion   *string `json:"http_version"`
	JA4           *string `json:"ja4"`
	JA4R          *string `json:"ja4_r"`
	PeetprintHash *string `json:"peetprint_hash"`
	AkamaiH2      *string `json:"akamai_h2"`
	AkamaiH2Hash  *string `json:"akamai_h2_hash"`
	UserAgent     *string `json:"user_agent"`
}

func launch(engine string, headless any, proxy string) (string, error) {
	b, _ := json.Marshal(map[string]any{"proxy": proxy, "headless": headless, "record": false})
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(b))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var s struct{ WsUrl string }
			json.Unmarshal(data, &s)
			return s.WsUrl, nil
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 5 {
			return "", fmt.Errorf("launch %d %s", res.StatusCode, data)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// A string field (nested path) of the echo's answer, or nil when missing or empty.
func str(j map[string]any, path ...string) *string {
	var v any = j
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	if s, ok := v.(string); ok && s != "" {
		return &s
	}
	return nil
}

type echo struct {
	Source, URL string
	Read        func(map[string]any) fingerprint
}

// The browser itself loads a TLS echo page, which answers with the ClientHello and HTTP/2 frames
// it received from this very browser. tls.peet.ws first; tls.browserleaks.com/json only if peet
// doesn't answer (it has had outages). Each is one page load in a fresh tab.
var echoes = []echo{
	{"tls.peet.ws", "https://tls.peet.ws/api/all", func(j map[string]any) fingerprint {
		return fingerprint{HTTPVersion: str(j, "http_version"), JA4: str(j, "tls", "ja4"), JA4R: str(j, "tls", "ja4_r"), PeetprintHash: str(j, "tls", "peetprint_hash"),
			AkamaiH2: str(j, "http2", "akamai_fingerprint"), AkamaiH2Hash: str(j, "http2", "akamai_fingerprint_hash"), UserAgent: str(j, "user_agent")}
	}},
	{"tls.browserleaks.com", "https://tls.browserleaks.com/json", func(j map[string]any) fingerprint {
		return fingerprint{JA4: str(j, "ja4"), JA4R: str(j, "ja4_r"), AkamaiH2: str(j, "akamai_text"), AkamaiH2Hash: str(j, "akamai_hash"), UserAgent: str(j, "user_agent")}
	}},
}

func measure(browser playwright.Browser) (fingerprint, error) {
	context, err := browser.NewContext()
	if err != nil {
		return fingerprint{}, err
	}
	last := ""
	for _, e := range echoes {
		tab, err := context.NewPage()
		if err != nil {
			return fingerprint{}, err
		}
		res, err := tab.Goto(e.URL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(30000)})
		var body string
		if err == nil {
			body, err = res.Text()
		}
		tab.Close()
		if err != nil {
			last = e.Source + ": " + strings.Split(err.Error(), "\n")[0]
			continue
		}
		var j map[string]any
		if err := json.Unmarshal([]byte(body), &j); err != nil {
			last = e.Source + ": " + err.Error()
			continue
		}
		fp := e.Read(j)
		if fp.JA4 != nil {
			fp.Source = e.Source
			return fp, nil
		}
		last = e.Source + ": no ja4 in the answer"
	}
	return fingerprint{}, errors.New(last)
}

func main() {
	pw, err := playwright.Run()
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	types := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}

	// One sticky residential exit for the whole run, and one session at a time.
	proxy := "cdpfleet-resi-country-us-session-c84" + runID + "-lifetime-30"
	fingerprints := map[string]any{}
	names := map[string]string{}
	for _, s := range setups {
		names[s.ID] = s.Name
		fail := func(err error) {
			msg := strings.Split(err.Error(), "\n")[0]
			if len(msg) > 200 {
				msg = msg[:200]
			}
			fingerprints[s.ID] = map[string]string{"error": msg}
		}
		ws, err := launch(s.Engine, s.Headless, proxy)
		if err != nil {
			fail(err)
			continue
		}
		browser, err := types[s.Type].Connect(ws, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		if err != nil {
			fail(err)
			continue
		}
		fp, err := measure(browser)
		version := browser.Version()
		browser.Close()
		if err != nil {
			fail(err)
			continue
		}
		fp.Version = version
		fingerprints[s.ID] = fp
	}

	// encoding/json sorts map keys; the setups' order is in the case's own table.
	out, _ := json.MarshalIndent(map[string]any{
		"measured_at":  time.Now().UTC().Format("2006-01-02"),
		"setups":       names,
		"fingerprints": fingerprints,
	}, "", "  ")
	fmt.Println(string(out))
}
