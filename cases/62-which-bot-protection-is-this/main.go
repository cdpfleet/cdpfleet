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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// Public pages, each loaded once: {url, expected vendor, demo name or ""}. Demo pages are the
// protection vendors' own test pages and are reported by name; for the rest only the counts per
// vendor are reported. The expected vendor is what a first survey (2026-10-19) found.
var pages = [][3]string{
	{"https://nowsecure.nl/", "turnstile", "nowsecure.nl (Turnstile test page)"},
	{"https://www.scrapingcourse.com/cloudflare-challenge", "cloudflare", "scrapingcourse.com Cloudflare challenge"},
	{"https://peet.ws/turnstile-test/non-interactive.html", "turnstile", "peet.ws Turnstile test (non-interactive)"},
	{"https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php", "recaptcha", "Google reCAPTCHA v3 demo"},
	{"https://accounts.hcaptcha.com/demo", "hcaptcha", "hCaptcha demo"},
	{"https://www.crunchbase.com/", "cloudflare", ""},
	{"https://discord.com/", "cloudflare", ""},
	{"https://www.glassdoor.com/", "cloudflare", ""},
	{"https://www.tripadvisor.com/", "datadome", ""},
	{"https://www.etsy.com/", "datadome", ""},
	{"https://soundcloud.com/", "datadome", ""},
	{"https://www.homedepot.com/", "akamai", ""},
	{"https://www.usps.com/", "akamai", ""},
	{"https://www.delta.com/", "akamai", ""},
	{"https://www.zillow.com/", "human", ""},
	{"https://www.walmart.com/", "human", ""},
	{"https://www.realestate.com.au/", "kasada", ""},
	{"https://www.ticketmaster.com/", "kasada", ""},
	{"https://www.amazon.com/", "awswaf", ""},
	{"https://www.booking.com/", "awswaf", ""},
}
var vendorOrder = []string{"cloudflare", "turnstile", "datadome", "akamai", "human", "kasada", "imperva", "awswaf", "recaptcha", "hcaptcha"}

// A lenient host match: url.Parse rejects some of the URLs pages request.
var hostRe = regexp.MustCompile(`^[a-z]+://([^/:?#]+)`)

func host(u string) string {
	if m := hostRe.FindStringSubmatch(u); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

func anyURL(urls []string, f func(string) bool) bool { return slices.ContainsFunc(urls, f) }
func anyHost(urls []string, re string) bool {
	r := regexp.MustCompile(re)
	return anyURL(urls, func(u string) bool { return r.MatchString(host(u)) })
}
func anyKey(h map[string]string, prefixes ...string) bool {
	for k := range h {
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				return true
			}
		}
	}
	return false
}
func has(h map[string]string, k string) bool { _, ok := h[k]; return ok }
func anyCookie(c []string, f func(string) bool) bool { return slices.ContainsFunc(c, f) }
func oneOf(names ...string) func(string) bool {
	return func(x string) bool { return slices.Contains(names, x) }
}

type signal struct {
	vendor, label string
	test          func(h map[string]string, c, u []string) bool
}

// Each vendor leaves response headers, cookies or requests to its own hosts.
var signals = []signal{
	{"cloudflare", "cf-ray header", func(h map[string]string, c, u []string) bool { return has(h, "cf-ray") }},
	{"cloudflare", "__cf_bm / cf_clearance cookie", func(h map[string]string, c, u []string) bool {
		return anyCookie(c, oneOf("__cf_bm", "cf_clearance", "__cflb"))
	}},
	{"cloudflare", "/cdn-cgi/challenge-platform request", func(h map[string]string, c, u []string) bool {
		return anyURL(u, func(x string) bool { return strings.Contains(x, "/cdn-cgi/challenge-platform") })
	}},
	{"turnstile", "Turnstile widget", func(h map[string]string, c, u []string) bool {
		return anyURL(u, func(x string) bool {
			return strings.Contains(x, "challenges.cloudflare.com") && strings.Contains(x, "turnstile")
		})
	}},
	{"datadome", "x-datadome header", func(h map[string]string, c, u []string) bool { return has(h, "x-datadome") || has(h, "x-dd-b") }},
	{"datadome", "datadome cookie", func(h map[string]string, c, u []string) bool { return slices.Contains(c, "datadome") }},
	{"datadome", "DataDome host (captcha-delivery.com, datadome.co)", func(h map[string]string, c, u []string) bool {
		return anyHost(u, `captcha-delivery\.com|datadome\.co`)
	}},
	{"akamai", "_abck / bm_sz cookie", func(h map[string]string, c, u []string) bool {
		return anyCookie(c, oneOf("_abck", "bm_sz", "ak_bmsc", "bm_sv"))
	}},
	{"akamai", "akamai header", func(h map[string]string, c, u []string) bool { return anyKey(h, "akamai", "x-akamai") }},
	{"human", "_px cookie", func(h map[string]string, c, u []string) bool {
		return anyCookie(c, func(x string) bool { return strings.HasPrefix(x, "_px") })
	}},
	{"human", "HUMAN host (px-cdn.net, px-cloud.net)", func(h map[string]string, c, u []string) bool {
		return anyHost(u, `px-cdn\.net|px-cloud\.net|perimeterx\.net|pxchk\.net`)
	}},
	{"kasada", "x-kpsdk header", func(h map[string]string, c, u []string) bool { return anyKey(h, "x-kpsdk") }},
	{"kasada", "ips.js / kpsdk request", func(h map[string]string, c, u []string) bool {
		return anyURL(u, func(x string) bool { return strings.Contains(x, "/ips.js") || strings.Contains(x, "kpsdk") })
	}},
	{"imperva", "x-iinfo header", func(h map[string]string, c, u []string) bool { return has(h, "x-iinfo") }},
	{"imperva", "incap_ses / visid_incap cookie", func(h map[string]string, c, u []string) bool {
		return anyCookie(c, func(x string) bool {
			return strings.HasPrefix(x, "incap_ses") || strings.HasPrefix(x, "visid_incap") || strings.HasPrefix(x, "nlbi_")
		})
	}},
	{"awswaf", "aws-waf-token cookie", func(h map[string]string, c, u []string) bool { return slices.Contains(c, "aws-waf-token") }},
	{"awswaf", "AWS WAF host (awswaf.com)", func(h map[string]string, c, u []string) bool { return anyHost(u, `awswaf\.com`) }},
	{"recaptcha", "reCAPTCHA script", func(h map[string]string, c, u []string) bool {
		return anyURL(u, func(x string) bool {
			return strings.Contains(x, "google.com/recaptcha") || strings.Contains(x, "recaptcha.net")
		})
	}},
	{"hcaptcha", "hCaptcha host", func(h map[string]string, c, u []string) bool { return anyHost(u, `hcaptcha\.com`) }},
}

var challengeRe = regexp.MustCompile(`(?i)just a moment|checking your browser|verify you are human|press & hold|press and hold|are you a robot|human touch`)
var blockedRe = regexp.MustCompile(`(?i)access denied|access to this page has been denied|attention required|you have been blocked|request blocked|error 1020|pardon our interruption`)
var spaceRe = regexp.MustCompile(`\s+`)

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

func head(s string, n int) string { return s[:min(n, len(s))] }

// One load in a fresh context: DOM ready + 6 s for the protection scripts to run, then the main
// response's headers, the cookies and every requested URL. Returns the outcome and the signals hit.
func survey(browser playwright.Browser, target string) (string, []signal) {
	context, err := browser.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	tab, err := context.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	var mu sync.Mutex
	urls := []string{}
	tab.OnRequest(func(r playwright.Request) {
		mu.Lock()
		defer mu.Unlock()
		urls = append(urls, r.URL())
	})
	h := map[string]string{}
	outcome := "error"
	if r, err := tab.Goto(target, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)}); err == nil {
		status := 0
		if r != nil {
			status = r.Status()
			if all, err := r.AllHeaders(); err == nil {
				h = all
			}
		}
		tab.WaitForTimeout(6000)
		title, _ := tab.Title()
		text, err := tab.Locator("body").InnerText()
		if err != nil {
			text = ""
		}
		text = spaceRe.ReplaceAllString(text, " ")
		frames := []string{}
		for _, f := range tab.Frames() {
			frames = append(frames, f.URL())
		}
		switch {
		case challengeRe.MatchString(title+" "+head(text, 800)) || strings.Contains(strings.Join(frames, " "), "captcha-delivery.com"):
			outcome = "challenge"
		case blockedRe.MatchString(title+" "+head(text, 400)) || (slices.Contains([]int{403, 429, 444}, status) && len(text) < 1500):
			outcome = "blocked"
		default:
			outcome = "content"
		}
	}
	cookies := []string{}
	if cs, err := context.Cookies(); err == nil {
		for _, c := range cs {
			cookies = append(cookies, c.Name)
		}
	}
	context.Close()
	mu.Lock()
	snapshot := slices.Clone(urls)
	mu.Unlock()
	hits := []signal{}
	for _, s := range signals {
		if s.test(h, cookies, snapshot) {
			hits = append(hits, s)
		}
	}
	return outcome, hits
}

type outcomes struct {
	Content   int `json:"content"`
	Challenge int `json:"challenge"`
	Blocked   int `json:"blocked"`
	Error     int `json:"error"`
}

func (o *outcomes) add(outcome string) {
	switch outcome {
	case "content":
		o.Content++
	case "challenge":
		o.Challenge++
	case "blocked":
		o.Blocked++
	default:
		o.Error++
	}
}

type vendor struct {
	Vendor   string         `json:"vendor"`
	Pages    int            `json:"pages"`
	Signals  map[string]int `json:"signals"`
	Outcomes outcomes       `json:"outcomes"`
}

type demoPage struct {
	Page    string   `json:"page"`
	Vendors []string `json:"vendors"`
	Outcome string   `json:"outcome"`
}

func main() {
	session, err := launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
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

	vendors := map[string]*vendor{}
	for _, v := range vendorOrder {
		vendors[v] = &vendor{Vendor: v, Signals: map[string]int{}}
	}
	demoPages := []demoPage{}
	none, expectedMatched := 0, 0
	for _, page := range pages {
		outcome, hits := survey(browser, page[0])
		found := []string{}
		for _, s := range hits {
			if !slices.Contains(found, s.vendor) {
				found = append(found, s.vendor)
			}
			vendors[s.vendor].Signals[s.label]++
		}
		if len(found) == 0 {
			none++
		}
		if slices.Contains(found, page[1]) {
			expectedMatched++
		}
		for _, v := range found {
			vendors[v].Pages++
			vendors[v].Outcomes.add(outcome)
		}
		if page[2] != "" {
			demoPages = append(demoPages, demoPage{page[2], found, outcome})
		}
	}
	list := []*vendor{}
	for _, v := range vendorOrder {
		if vendors[v].Pages > 0 {
			list = append(list, vendors[v])
		}
	}
	out, _ := json.MarshalIndent(map[string]any{
		"measured_at":           time.Now().UTC().Format("2006-01-02"),
		"pages":                 len(pages),
		"no_vendor_found":       none,
		"expected_vendor_found": expectedMatched,
		"vendors":               list,
		"demo_pages":            demoPages,
	}, "", "  ")
	fmt.Println(string(out))
}
