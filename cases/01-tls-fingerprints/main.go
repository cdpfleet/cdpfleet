// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL (http://user:pass@host:port)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

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

type row struct {
	Browser       string  `json:"browser"`
	Version       string  `json:"version"`
	UserAgent     string  `json:"user_agent"`
	HTTPVersion   string  `json:"http_version"`
	JA4           string  `json:"ja4"`
	JA3Hash       string  `json:"ja3_hash"`
	PeetprintHash string  `json:"peetprint_hash"`
	AkamaiH2      *string `json:"akamai_h2"`
	AkamaiH2Hash  *string `json:"akamai_h2_hash"`
	CipherSuites  int     `json:"cipher_suites"`
	Extensions    int     `json:"extensions"`
}

// The part of tls.peet.ws/api/all we use.
type peet struct {
	UserAgent   string `json:"user_agent"`
	HTTPVersion string `json:"http_version"`
	TLS         struct {
		JA4           string `json:"ja4"`
		JA3Hash       string `json:"ja3_hash"`
		PeetprintHash string `json:"peetprint_hash"`
		Ciphers       []any  `json:"ciphers"`
		Extensions    []any  `json:"extensions"`
	} `json:"tls"`
	HTTP2 *struct {
		AkamaiFingerprint     string `json:"akamai_fingerprint"`
		AkamaiFingerprintHash string `json:"akamai_fingerprint_hash"`
	} `json:"http2"`
}

// Navigate the browser itself: the JSON describes the TLS ClientHello and HTTP/2
// frames this very browser sent (APIRequest would use Playwright's own client).
// A residential exit occasionally times out: one retry, in a fresh tab.
func fingerprint(browser playwright.Browser) (fp peet, err error) {
	for attempt := 1; attempt <= 2; attempt++ {
		page, _ := browser.NewPage()
		var res playwright.Response
		if res, err = page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(30000)}); err == nil {
			return fp, res.JSON(&fp)
		}
	}
	return fp, err
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	// One browser per engine family, and the Playwright client that speaks to it.
	browsers := []struct {
		name   string
		family playwright.BrowserType
	}{{"chrome", pw.Chromium}, {"edge", pw.Chromium}, {"firefox", pw.Firefox}, {"camoufox", pw.Firefox}, {"webkit", pw.WebKit}}

	rows := []row{}
	for _, b := range browsers {
		session, err := launch(b.name, map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
		if err != nil {
			log.Fatal(err)
		}
		browser, err := b.family.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		if err != nil {
			log.Fatal(err)
		}
		fp, err := fingerprint(browser)
		if err != nil {
			log.Fatal(err)
		}
		r := row{Browser: b.name, Version: browser.Version(), UserAgent: fp.UserAgent, HTTPVersion: fp.HTTPVersion,
			JA4: fp.TLS.JA4, JA3Hash: fp.TLS.JA3Hash, PeetprintHash: fp.TLS.PeetprintHash,
			CipherSuites: len(fp.TLS.Ciphers), Extensions: len(fp.TLS.Extensions)}
		if fp.HTTP2 != nil {
			r.AkamaiH2, r.AkamaiH2Hash = &fp.HTTP2.AkamaiFingerprint, &fp.HTTP2.AkamaiFingerprintHash
		}
		rows = append(rows, r)
		browser.Close() // ends the session and stops billing
	}
	out, _ := json.MarshalIndent(rows, "", "  ")
	fmt.Println(string(out))
}
