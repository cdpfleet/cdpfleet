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
	"sync"

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

type result struct {
	Variant        string  `json:"variant"`
	CatalogVersion string  `json:"catalog_version,omitempty"`
	BrowserVersion string  `json:"browser_version,omitempty"`
	UserAgent      string  `json:"user_agent,omitempty"`
	SecChUA        *string `json:"sec_ch_ua,omitempty"`
	JA4            string  `json:"ja4,omitempty"`
	AkamaiH2Hash   string  `json:"akamai_h2_hash,omitempty"`
	Error          string  `json:"error,omitempty"`
}

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

func probe(pw *playwright.Playwright, label string, options map[string]any, expected string) result {
	// headless: false (a real display) so the user agent doesn't say "HeadlessChrome".
	options["proxy"], options["headless"] = os.Getenv("PROXY_URL"), false
	session, err := launch("chrome", options)
	if err != nil {
		return result{Variant: label, Error: err.Error()}
	}
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return result{Variant: label, Error: err.Error()}
	}
	defer browser.Close()
	page, _ := browser.NewPage()
	res, err := page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
	if err != nil { // a residential exit occasionally times out: one retry, in a fresh tab
		page, _ = browser.NewPage()
		res, err = page.Goto("https://tls.peet.ws/api/all", playwright.PageGotoOptions{Timeout: playwright.Float(30000)})
	}
	if err != nil {
		return result{Variant: label, Error: err.Error()}
	}
	var fp peet
	res.JSON(&fp)
	r := result{Variant: label, CatalogVersion: expected, BrowserVersion: browser.Version(), UserAgent: fp.UserAgent,
		JA4: fp.TLS.JA4, AkamaiH2Hash: fp.HTTP2.AkamaiFingerprintHash}
	for _, f := range fp.HTTP2.SentFrames {
		for _, h := range f.Headers {
			if f.FrameType == "HEADERS" && strings.HasPrefix(h, "sec-ch-ua: ") {
				v := h[11:]
				r.SecChUA = &v
			}
		}
	}
	return r
}

func main() {
	// The live catalog says which channels and previous majors exist right now.
	res, err := http.Get("https://cdpfleet.com/api/public/browsers")
	if err != nil {
		log.Fatal(err)
	}
	var catalog struct {
		Engines []struct {
			Key      string                            `json:"key"`
			Versions []struct{ Label, Version string } `json:"versions"`
		} `json:"engines"`
	}
	json.NewDecoder(res.Body).Decode(&catalog)
	res.Body.Close()

	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()

	var jobs []func() result
	for _, e := range catalog.Engines {
		if e.Key != "chrome" {
			continue
		}
		for _, v := range e.Versions {
			v := v
			label, options := "channel "+v.Label, map[string]any{}
			if v.Label == "pinned" {
				major := strings.Split(v.Version, ".")[0]
				label, options["version"] = "version "+major, major
			} else if v.Label != "stable" {
				options["channel"] = v.Label
			}
			jobs = append(jobs, func() result { return probe(pw, label, options, v.Version) })
		}
	}
	// All variants at once: each is its own session.
	results := make([]result, len(jobs))
	var wg sync.WaitGroup
	for i, job := range jobs {
		wg.Add(1)
		go func(i int, job func() result) { defer wg.Done(); results[i] = job() }(i, job)
	}
	wg.Wait()
	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
}
