// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY (sticky US exits from the cdpfleet residential proxy, Tier 1; one load per bot-protection page)
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")
var runID = func() string { b := make([]byte, 3); rand.Read(b); return hex.EncodeToString(b) }()

// One sticky exit per setup, used with and without the tunnel: the only difference between the two rows is vpn_tunnel.
func exitFor(id string) string { return "cdpfleet-resi-country-us-session-t" + id + runID + "-lifetime-30" }

type setup struct {
	ID, Name, Engine, Type string
	Headless               bool
}

var setups = []setup{
	{"c", "Google Chrome, headful", "chrome", "chromium", false},
	{"f", "Camoufox, defaults", "camoufox", "firefox", true},
}

type network struct {
	Label  string
	Tunnel bool
}

var networks = []network{{"residential proxy", false}, {"residential proxy + vpn_tunnel", true}}

// The WebRTC leak test from case 54: ICE candidates against a public STUN server. A "srflx" candidate is the public IP
// the STUN server saw. The same expression runs in every language.
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

// The protocol of the page's own request: h2 over TCP, or h3 over QUIC (UDP).
const protocol = `performance.getEntriesByType('navigation')[0]?.nextHopProtocol || null`

// Cloudflare's interstitial or block page, or the real page.
const cfState = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  return /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
})()`

// DataDome's iframe from captcha-delivery.com: a device check, a CAPTCHA, or a block (t=bv) — as in case 65.
const ddState = `(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()`

var ipv4 = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)

type cell struct {
	Outcome string `json:"outcome"`
	Seconds *int   `json:"seconds"`
}

type row struct {
	Setup          string    `json:"setup"`
	Network        string    `json:"network"`
	ExitIP         string    `json:"exit_ip"`
	WebrtcAPI      bool      `json:"webrtc_api"`
	WebrtcIPs      []string  `json:"webrtc_ips"`
	MdnsOnly       bool      `json:"mdns_only"`
	Protocols      []*string `json:"protocols"`
	Scrapingcourse cell      `json:"scrapingcourse"`
	Datadome       cell      `json:"datadome"`
}

var pw *playwright.Playwright

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(s setup, n network) playwright.Browser {
	body := map[string]any{"proxy": exitFor(s.ID), "headless": s.Headless, "record": false}
	if n.Tunnel {
		body["vpn_tunnel"] = true
	}
	payload, _ := json.Marshal(body)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+s.Engine+"/session", bytes.NewReader(payload))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session struct{ WsURL string }
			json.Unmarshal(msg, &session)
			bt := pw.Chromium
			if s.Type == "firefox" {
				bt = pw.Firefox
			}
			browser, err := bt.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
			if err != nil {
				log.Fatal(err)
			}
			return browser
		}
		if (res.StatusCode != 429 && res.StatusCode != 503) || attempt == 6 {
			log.Fatalf("launch %d %s", res.StatusCode, msg)
		}
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

// Residential peers drop a connection now and then: one retry.
func goTo(tab playwright.Page, url string) error {
	opts := playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)}
	if _, err := tab.Goto(url, opts); err == nil {
		return nil
	}
	_, err := tab.Goto(url, opts)
	return err
}

func str(tab playwright.Page, expr string) string {
	v, err := tab.Evaluate(expr)
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// One load, then watch the page once a second for 20 s (Cloudflare) or 12 s (DataDome) without touching it.
func watch(ctx playwright.BrowserContext, url, kind string) cell {
	tab, _ := ctx.NewPage()
	defer tab.Close()
	t0 := time.Now()
	if err := goTo(tab, url); err != nil {
		return cell{"error", nil}
	}
	seen := false
	window, expr := 12*time.Second, ddState
	if kind == "cf" {
		window, expr = 20*time.Second, cfState
	}
	until := time.Now().Add(window)
	for time.Now().Before(until) {
		s := str(tab, expr)
		if s == "blocked" {
			return cell{"blocked", nil}
		}
		if s == "challenge" && kind == "dd" {
			return cell{"challenge", nil}
		}
		if s == "challenge" || s == "checking" {
			seen = true
		}
		if s == "page" {
			if seen {
				sec := int(math.Round(time.Since(t0).Seconds()))
				return cell{"cleared", &sec}
			}
			return cell{"content", nil}
		}
		time.Sleep(time.Second)
	}
	if seen {
		return cell{"challenge", nil}
	}
	return cell{"error", nil}
}

func measure(s setup, n network) row {
	browser := launch(s, n)
	defer browser.Close()
	out := row{Setup: s.Name, Network: n.Label, WebrtcIPs: []string{}, Protocols: []*string{}}
	ctx, _ := browser.NewContext()
	tab, _ := ctx.NewPage()
	if err := goTo(tab, "https://api.ipify.org/?format=json"); err != nil {
		log.Fatal(err)
	}
	text, _ := tab.TextContent("body")
	var ip struct{ IP string }
	json.Unmarshal([]byte(text), &ip)
	out.ExitIP = ip.IP
	v, err := tab.Evaluate("(" + gather + ")()")
	if err != nil {
		log.Fatal(err)
	}
	g := v.(map[string]any)
	out.WebrtcAPI, _ = g["api"].(bool)
	sawMdns := false
	cands, _ := g["candidates"].([]any)
	for _, c := range cands {
		// candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
		f := strings.Split(c.(string), " ")
		if len(f) < 8 {
			continue
		}
		addr := f[4]
		if strings.HasSuffix(addr, ".local") {
			sawMdns = true
		}
		if ipv4.MatchString(addr) && addr != "0.0.0.0" {
			dup := false
			for _, x := range out.WebrtcIPs {
				dup = dup || x == addr
			}
			if !dup {
				out.WebrtcIPs = append(out.WebrtcIPs, addr)
			}
		}
	}
	out.MdnsOnly = len(out.WebrtcIPs) == 0 && sawMdns
	tab.Close()
	// HTTP/3: a page announces it over HTTP/2 (Alt-Svc); the browser can switch to QUIC on a later request. Four loads
	// in fresh tabs, a pause between them.
	for i := 0; i < 4; i++ {
		t, _ := ctx.NewPage()
		goTo(t, "https://cloudflare-quic.com/")
		var p *string
		if s := str(t, protocol); s != "" {
			p = &s
		}
		out.Protocols = append(out.Protocols, p)
		t.Close()
		time.Sleep(2500 * time.Millisecond)
	}
	ctx.Close()
	// The bot-protection pages, each in a fresh context: Cloudflare's challenge test page (case 64) and DataDome's own
	// site (case 65).
	c1, _ := browser.NewContext()
	out.Scrapingcourse = watch(c1, "https://www.scrapingcourse.com/cloudflare-challenge", "cf")
	c1.Close()
	c2, _ := browser.NewContext()
	out.Datadome = watch(c2, "https://datadome.co/", "dd")
	c2.Close()
	return out
}

func main() {
	var err error
	pw, err = playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	rows := []row{}
	for _, s := range setups {
		for _, n := range networks {
			rows = append(rows, measure(s, n))
		}
	}
	out, _ := json.MarshalIndent(map[string]any{"measured_at": time.Now().UTC().Format("2006-01-02"), "rows": rows}, "", "  ")
	fmt.Println(string(out))
}
