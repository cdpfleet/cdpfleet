// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (each setup gets its own sticky US exit from the cdpfleet residential proxy; a few KB of traffic)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// What a WebRTC leak test runs: gather ICE candidates against a public STUN server. A "srflx"
// candidate carries the public IP the STUN server saw; UDP doesn't go through an HTTP/SOCKS proxy —
// on cdpfleet, proxied sessions can't send UDP at all, so only an anonymous mDNS host candidate is left.
const gather = `async () => {
  if (typeof RTCPeerConnection === 'undefined') return { api: false, candidates: [] };
  const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
  const candidates = [];
  pc.onicecandidate = (e) => { if (e.candidate && e.candidate.candidate) candidates.push(e.candidate.candidate); };
  pc.createDataChannel('probe');
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise((done) => {
    const timer = setTimeout(done, 8000);
    pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === 'complete') { clearTimeout(timer); done(); } };
  });
  pc.close();
  return { api: true, candidates };
}`

type setup struct {
	Name   string
	Engine string
	Body   map[string]any
}

var setups = []setup{
	{"Chromium", "chromium", map[string]any{"headless": "new"}},
	{"Google Chrome", "chrome", map[string]any{"headless": "new"}},
	{"Firefox", "firefox", map[string]any{"headless": true}},
	{"Camoufox (defaults: geoip on)", "camoufox", map[string]any{"headless": true}},
	{"Camoufox, block_webrtc: true", "camoufox", map[string]any{"headless": true, "block_webrtc": true}},
}

type result struct {
	Setup              string   `json:"setup"`
	ExitIP             string   `json:"exit_ip"`
	WebRTCAPI          bool     `json:"webrtc_api"`
	Candidates         int      `json:"candidates"`
	MDNSHostCandidates int      `json:"mdns_host_candidates"`
	PublicIPs          []string `json:"public_ips"`
	ShowsExitIP        bool     `json:"shows_exit_ip"`
	LeaksOtherIP       bool     `json:"leaks_other_ip"`
}

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

// Residential peers drop a connection now and then: up to two retries.
func goTo(page playwright.Page, url string) error {
	for attempt := 1; ; attempt++ {
		_, err := page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil || attempt == 3 {
			return err
		}
		time.Sleep(2 * time.Second)
	}
}

var ipv4 = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

func one(pw *playwright.Playwright, s setup, proxy string) (result, error) {
	out := result{Setup: s.Name, PublicIPs: []string{}}
	opts := map[string]any{"proxy": proxy, "record": false}
	for k, v := range s.Body {
		opts[k] = v
	}
	session, err := launch(s.Engine, opts)
	if err != nil {
		return out, err
	}
	browserType := pw.Chromium
	if s.Engine == "firefox" || s.Engine == "camoufox" {
		browserType = pw.Firefox
	}
	browser, err := browserType.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()
	context, err := browser.NewContext()
	if err != nil {
		return out, err
	}
	page, err := context.NewPage()
	if err != nil {
		return out, err
	}
	if err := goTo(page, "https://api.ipify.org/?format=json"); err != nil {
		return out, err
	}
	text, err := page.TextContent("body")
	if err != nil {
		return out, err
	}
	var ip struct {
		IP string `json:"ip"`
	}
	if err := json.Unmarshal([]byte(text), &ip); err != nil {
		return out, err
	}
	out.ExitIP = ip.IP
	raw, err := page.Evaluate(gather)
	if err != nil {
		return out, err
	}
	gathered := raw.(map[string]any)
	out.WebRTCAPI = gathered["api"].(bool)
	candidates, _ := gathered["candidates"].([]any)
	out.Candidates = len(candidates)
	seen := map[string]bool{}
	for _, c := range candidates {
		// candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
		f := strings.Split(c.(string), " ")
		address, typ := f[4], f[7]
		if typ == "host" && strings.HasSuffix(address, ".local") {
			out.MDNSHostCandidates++
		}
		if ipv4.MatchString(address) && address != "0.0.0.0" && !seen[address] {
			seen[address] = true
			out.PublicIPs = append(out.PublicIPs, address)
		}
	}
	for _, a := range out.PublicIPs {
		if a == out.ExitIP {
			out.ShowsExitIP = true
		} else {
			out.LeaksOtherIP = true
		}
	}
	return out, nil
}

func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	// A sticky residential exit per setup: one IP for the whole session, to compare with.
	run := strconv.FormatInt(rand.Int63(), 36)[:6]
	results := []result{}
	for i, s := range setups {
		r, err := one(pw, s, fmt.Sprintf("cdpfleet-resi-country-us-session-w54%s%d-lifetime-10", run, i))
		if err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
		results = append(results, r)
	}
	b, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(b))
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
