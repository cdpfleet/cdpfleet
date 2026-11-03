// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (your plan), PROXY_URL; optional COMMUNITY_API_KEY, CDPFLEET_DEDICATED_API_KEY to compare plans
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const starter = "https://starter.cdpfleet.com"
const api = "https://cdpfleet.com"
const pageURL = "https://en.wikipedia.org/wiki/World_War_II" // a long article: a mid-weight page
const maxTabs = 50                                           // the per-session tab cap on every plan

var proxy = os.Getenv("PROXY_URL")

// Each run: an account, an engine and a memory multiplier. Runs whose key isn't set are skipped.
var runs = []struct {
	env, engine string
	memory      int
}{
	{"COMMUNITY_API_KEY", "chromium", 1},
	{"CDPFLEET_API_KEY", "chromium", 1},
	{"CDPFLEET_API_KEY", "firefox", 1},
	{"CDPFLEET_API_KEY", "webkit", 1},
	{"CDPFLEET_API_KEY", "chromium", 2},
	{"CDPFLEET_DEDICATED_API_KEY", "chromium", 1},
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func getJSON(key, path string, out any) {
	req, _ := http.NewRequest("GET", api+path, nil)
	req.Header.Set("x-api-key", key)
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		log.Fatal(err)
	}
}

func firstLine(err error) string { return strings.SplitN(err.Error(), "\n", 2)[0] }

type record struct {
	SessionID    string  `json:"session_id"`
	EndedAt      *string `json:"ended_at"`
	EndReason    *string `json:"end_reason"`
	Weight       int     `json:"weight"`
	MemoryPeakMB *int    `json:"memory_peak_mb"`
	Duration     int     `json:"duration_seconds"`
}

type result struct {
	Plan          string  `json:"plan"`
	Engine        string  `json:"engine"`
	Memory        int     `json:"memory"`
	LaunchError   string  `json:"launch_error,omitempty"`
	Weight        int     `json:"weight"`
	MemoryMB      int     `json:"memory_mb"`
	Tabs          int     `json:"tabs"`
	ClientError   *string `json:"client_error"`
	Tab51         *string `json:"tab_51"`
	EndReason     *string `json:"end_reason"`
	MemoryPeakMB  *int    `json:"memory_peak_mb"`
	Seconds       int     `json:"seconds"`
	ThreadSeconds *int    `json:"thread_seconds"`
}

var sessionClosed = regexp.MustCompile(`(Session closed: .*)$`)

func run(pw *playwright.Playwright, env, engine string, memory int) result {
	key := os.Getenv(env)
	var me struct {
		Subscription struct {
			Kind     string `json:"kind"`
			Features struct {
				Community bool `json:"community"`
			} `json:"features"`
		} `json:"subscription"`
	}
	getJSON(key, "/v1/me", &me)
	plan := "Shared"
	if me.Subscription.Features.Community {
		plan = "Community"
	} else if me.Subscription.Kind == "dedicated" {
		plan = "Dedicated"
	}
	out := result{Plan: plan, Engine: engine, Memory: memory}
	t0 := time.Now()
	// Community launches use the plan's included proxy; paid plans bring their own.
	body := map[string]any{"headless": true}
	if memory > 1 {
		body["memory"] = memory
	}
	if plan != "Community" {
		body["proxy"] = proxy
	}
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", starter+"/"+engine+"/session", bytes.NewReader(payload))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if res.StatusCode != 200 {
		var b bytes.Buffer
		b.ReadFrom(res.Body)
		out.LaunchError = fmt.Sprintf("%d %s", res.StatusCode, b.String())
		return out
	}
	var launch struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
		Weight    int    `json:"weight"`
		MemoryMB  int    `json:"memory_mb"`
	}
	if err := json.NewDecoder(res.Body).Decode(&launch); err != nil {
		log.Fatal(err)
	}
	types := map[string]playwright.BrowserType{"chromium": pw.Chromium, "firefox": pw.Firefox, "webkit": pw.WebKit}
	browser := must(types[engine].Connect(launch.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	context := must(browser.NewContext())
	// Open the article in one new tab after another until the session ends or the tab cap is reached.
	tabs := 0
	var clientError *string
	for tabs < maxTabs {
		page, err := context.NewPage()
		if err == nil {
			_, err = page.Goto(pageURL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
			// A slow load through a shared proxy gets one more try; a closed session doesn't.
			if err != nil && strings.Contains(err.Error(), "Timeout") {
				_, err = page.Goto(pageURL, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
			}
		}
		if err != nil {
			msg := firstLine(err)
			if m := sessionClosed.FindStringSubmatch(msg); m != nil {
				msg = m[1]
			}
			clientError = &msg
			break
		}
		tabs++
	}
	// At the cap with memory to spare: what does a 51st tab get?
	var tab51 *string
	if tabs == maxTabs {
		msg := "opened"
		if _, err := context.NewPage(); err != nil {
			msg = firstLine(err)
		}
		tab51 = &msg
	}
	browser.Close()
	// The session's record: why it ended, its weight, peak memory and time.
	var rec *record
	for i := 0; i < 20 && (rec == nil || rec.EndedAt == nil); i++ {
		time.Sleep(1500 * time.Millisecond)
		var r struct {
			Sessions []record `json:"sessions"`
		}
		getJSON(key, "/v1/me/sessions?limit=20", &r)
		for _, s := range r.Sessions {
			if s.SessionID == launch.SessionID {
				s := s
				rec = &s
			}
		}
	}
	out.Weight, out.MemoryMB, out.Tabs, out.ClientError, out.Tab51 = launch.Weight, launch.MemoryMB, tabs, clientError, tab51
	out.Seconds = int(time.Since(t0).Seconds())
	if rec != nil {
		ts := rec.Duration * rec.Weight
		out.EndReason, out.MemoryPeakMB, out.Seconds, out.ThreadSeconds = rec.EndReason, rec.MemoryPeakMB, rec.Duration, &ts
	}
	return out
}

func main() {
	pw := must(playwright.Run())
	defer pw.Stop()
	results := []result{}
	for _, r := range runs { // one at a time: one site, one session at a time
		if os.Getenv(r.env) != "" {
			results = append(results, run(pw, r.env, r.engine, r.memory))
		}
	}
	doc := map[string]any{"language": "go", "page": pageURL, "max_tabs": maxTabs, "measured_at": time.Now().UTC().Format("2006-01-02"), "runs": results}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		log.Fatal(err)
	}
}
