// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy), PROXY_URL_US (a US proxy)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

var sites = []string{"https://www.theguardian.com/international", "https://www.bbc.com/", "https://apnews.com/", "https://www.theverge.com/", "https://www.huffpost.com/"}

// First-layer buttons of the two consent platforms these sites use. OneTrust renders in the
// page; Sourcepoint in an iframe. Only a free "reject" counts: "Reject all and subscribe" doesn't.
var reject = map[string]string{"OneTrust": "#onetrust-reject-all-handler", "Sourcepoint": `button[title="Reject all"], button[title="I do not agree"]`}
var accept = map[string]string{"OneTrust": "#onetrust-accept-btn-handler", "Sourcepoint": `button[title="Accept all"], button[title="I agree"]`}

const spFrame = `iframe[id^="sp_message_iframe"]`
const banner = `#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]`

// Which consent platform the page runs, whether its banner is showing, and which consent
// APIs it exposes (__tcfapi: IAB Europe's TCF; __gpp: IAB Tech Lab's GPP, used for US state
// privacy laws).
const probe = `() => {
  const shown = (s) => [...document.querySelectorAll(s)].some((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
  const cmp = window.OneTrust || document.querySelector('#onetrust-consent-sdk') ? 'OneTrust'
    : window._sp_ || document.querySelector('iframe[id^="sp_message_iframe"]') ? 'Sourcepoint'
    : window.Didomi ? 'Didomi' : window.Cookiebot ? 'Cookiebot' : window.UC_UI ? 'Usercentrics' : null;
  return { cmp, banner: shown('#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'),
    tcf: typeof window.__tcfapi === 'function', gpp: typeof window.__gpp === 'function' };
}`

type before struct {
	Status            *int   `json:"status"`
	CMP               any    `json:"cmp"`
	Banner            bool   `json:"banner"`
	TCF               bool   `json:"tcf"`
	GPP               bool   `json:"gpp"`
	BannerAfterMs     *int64 `json:"banner_after_ms"`
	Cookies           int    `json:"cookies"`
	ThirdPartyCookies int    `json:"third_party_cookies"`
	OtherHosts        int    `json:"other_hosts"`
}

type after struct {
	Button            string `json:"button"`
	Clicks            int    `json:"clicks"`
	Cookies           int    `json:"cookies"`
	ThirdPartyCookies int    `json:"third_party_cookies"`
	OtherHosts        int    `json:"other_hosts"`
}

type siteResult struct {
	Site            string  `json:"site"`
	US              *before `json:"us"`
	DE              *before `json:"de"`
	DEReject        *after  `json:"de_reject"`
	DERejectMissing *string `json:"de_reject_missing"`
	DEAccept        *after  `json:"de_accept"`
	DEAcceptMissing *string `json:"de_accept_missing"`
}

func launch(proxy string) (string, error) {
	body, _ := json.Marshal(map[string]any{"proxy": proxy, "headless": false})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("launch: %s %s", res.Status, msg)
	}
	var session struct {
		WsURL string `json:"wsUrl"`
	}
	return session.WsURL, json.NewDecoder(res.Body).Decode(&session)
}

func domainOf(u string) string {
	parsed, _ := url.Parse(u)
	parts := strings.Split(parsed.Hostname(), ".")
	return strings.Join(parts[len(parts)-2:], ".")
}

func onDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// press clicks a consent button and reports whether the click went through (one retry: a
// consent iframe that re-renders can swallow a click).
func press(button playwright.Locator) bool {
	for attempt := 1; attempt <= 2; attempt++ {
		if button.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(10000)}) == nil {
			return true
		}
	}
	return false
}

// visit loads a site in a fresh context, then optionally clicks a consent button. It counts
// cookies and the hosts on other domains the page contacted (ads, analytics, CDNs) up to each
// moment. after is nil (and missing says why) when there was nothing to click.
func visit(browser playwright.Browser, site, choice string) (*before, *after, *string, error) {
	domain := domainOf(site)
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{Viewport: &playwright.Size{Width: 1280, Height: 900}})
	if err != nil {
		return nil, nil, nil, err
	}
	defer ctx.Close()
	page, err := ctx.NewPage()
	if err != nil {
		return nil, nil, nil, err
	}
	var mu sync.Mutex
	hosts := map[string]bool{}
	page.On("request", func(r playwright.Request) {
		u, err := url.Parse(r.URL())
		if err != nil || u.Hostname() == "" || onDomain(u.Hostname(), domain) {
			return
		}
		mu.Lock()
		hosts[u.Hostname()] = true
		mu.Unlock()
	})
	measure := func() (int, int, int, error) {
		cookies, err := ctx.Cookies()
		if err != nil {
			return 0, 0, 0, err
		}
		third := 0
		for _, c := range cookies {
			if !onDomain(strings.TrimPrefix(c.Domain, "."), domain) {
				third++
			}
		}
		mu.Lock()
		defer mu.Unlock()
		return len(cookies), third, len(hosts), nil
	}

	load := playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(60000)}
	res, err := page.Goto(site, load)
	if err != nil { // proxies drop a connection now and then: one retry
		if res, err = page.Goto(site, load); err != nil {
			return nil, nil, nil, err
		}
	}
	// Consent scripts load late on busy pages: wait up to 30 s for the banner, and give every
	// page at least 6 s before measuring.
	t0 := time.Now()
	var bannerAfterMs *int64
	if page.Locator(banner).First().WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(30000)}) == nil {
		ms := time.Since(t0).Milliseconds()
		bannerAfterMs = &ms
	}
	if elapsed := time.Since(t0).Milliseconds(); elapsed < 6000 {
		page.WaitForTimeout(float64(6000 - elapsed))
	}
	v, err := page.Evaluate("(" + probe + ")()")
	if err != nil {
		return nil, nil, nil, err
	}
	m := v.(map[string]any)
	b := &before{CMP: m["cmp"], Banner: m["banner"] == true, TCF: m["tcf"] == true, GPP: m["gpp"] == true, BannerAfterMs: bannerAfterMs}
	if res != nil {
		status := res.Status()
		b.Status = &status
	}
	if b.Cookies, b.ThirdPartyCookies, b.OtherHosts, err = measure(); err != nil {
		return nil, nil, nil, err
	}
	if choice == "" {
		return b, nil, nil, nil
	}
	missing := "no banner"
	if b.Banner {
		missing = "no " + choice + " button on the first layer"
	}
	cmp, _ := m["cmp"].(string)
	selectors := accept
	if choice == "reject" {
		selectors = reject
	}
	selector, ok := selectors[cmp]
	if !ok {
		return b, nil, &missing, nil
	}
	var button playwright.Locator
	if cmp == "Sourcepoint" {
		button = page.FrameLocator(spFrame).Locator(selector).First()
	} else {
		button = page.Locator(selector).First()
	}
	if err := button.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible, Timeout: playwright.Float(10000)}); err != nil {
		return b, nil, &missing, nil
	}
	label, err := button.GetAttribute("title")
	if err != nil || label == "" {
		text, err := button.InnerText()
		if err != nil {
			return nil, nil, nil, err
		}
		label = strings.TrimSpace(text)
	}
	page.WaitForTimeout(2000) // let the banner finish animating in, or the click can be lost
	if !press(button) {
		failed := "click failed"
		return b, nil, &failed, nil
	}
	// If the banner is still up, the click was lost: click once more.
	clicks := 1
	if err := page.Locator(banner).First().WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateHidden, Timeout: playwright.Float(5000)}); err != nil {
		clicks = 2
		press(button)
	}
	page.WaitForTimeout(6000)
	a := &after{Button: label, Clicks: clicks}
	if a.Cookies, a.ThirdPartyCookies, a.OtherHosts, err = measure(); err != nil {
		return nil, nil, nil, err
	}
	return b, a, nil, nil
}

// visitOnce: a site may refuse one exit IP (an error status), so visit again in a fresh
// context, whose new proxy connection gets another IP from the rotating proxy.
func visitOnce(browser playwright.Browser, site, choice string) (*before, *after, *string, error) {
	b, a, missing, err := visit(browser, site, choice)
	if err != nil || (b.Status != nil && *b.Status/100 == 2) {
		return b, a, missing, err
	}
	return visit(browser, site, choice)
}

func runUS(us playwright.Browser) ([]*before, error) {
	results := []*before{}
	for _, site := range sites {
		b, _, _, err := visitOnce(us, site, "")
		if err != nil {
			return nil, err
		}
		results = append(results, b)
	}
	return results, nil
}

func runDE(de playwright.Browser, fromUs []*before) ([]siteResult, error) {
	results := []siteResult{}
	for i, site := range sites {
		deBefore, rejected, rejectMissing, err := visitOnce(de, site, "reject")
		if err != nil {
			return nil, err
		}
		_, accepted, acceptMissing, err := visitOnce(de, site, "accept")
		if err != nil {
			return nil, err
		}
		u, _ := url.Parse(site)
		results = append(results, siteResult{Site: strings.TrimPrefix(u.Hostname(), "www."), US: fromUs[i], DE: deBefore,
			DEReject: rejected, DERejectMissing: rejectMissing, DEAccept: accepted, DEAcceptMissing: acceptMissing})
	}
	return results, nil
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	fatal := func(err error) { // log.Fatal would skip the deferred pw.Stop()
		pw.Stop()
		log.Fatal(err)
	}
	connect := func(proxy string) playwright.Browser {
		wsURL, err := launch(proxy)
		if err != nil {
			fatal(err)
		}
		browser, err := pw.Chromium.Connect(wsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
		if err != nil {
			fatal(err)
		}
		return browser
	}
	// Sessions on shared threads end after 2 minutes without traffic by default, so the countries run one after the other.
	us := connect(os.Getenv("PROXY_URL_US"))
	fromUs, err := runUS(us)
	us.Close()
	if err != nil {
		fatal(err)
	}
	de := connect(os.Getenv("PROXY_URL_DE"))
	results, err := runDE(de, fromUs)
	de.Close()
	if err != nil {
		fatal(err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(map[string]any{"sites": results})
	fmt.Print(buf.String())
}
