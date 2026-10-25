// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const site = "https://authenticationtest.com"

// The practice site publishes its test account and TOTP secret on the challenge page.
const email, password, secret = "totp@authenticationtest.com", "pa$$w0rd", "I65VU7K5ZQL7WB4E"

var key = os.Getenv("CDPFLEET_API_KEY")

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps) from the decoded secret — what an authenticator app does.
func totp(k []byte, unixSeconds float64) string {
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(math.Floor(unixSeconds/30)))
	mac := hmac.New(sha1.New, k)
	mac.Write(counter)
	h := mac.Sum(nil)
	o := h[len(h)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(h[o:o+4])&0x7fffffff)%1000000)
}

func now() float64 { return float64(time.Now().UnixMilli()) / 1000 }

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// A code typed in the last seconds of its window can expire before the server checks it:
// wait for the next window when fewer than 5 s are left.
func freshWindow() (secondsLeft, waited float64) {
	left := 30 - math.Mod(now(), 30)
	if left < 5 {
		time.Sleep(time.Duration((left+0.2)*1000) * time.Millisecond)
		return round1(left), round1(left)
	}
	return round1(left), 0
}

func launch(pw *playwright.Playwright) (string, playwright.Browser, error) {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return "", nil, fmt.Errorf("launch: %s %s", res.Status, msg)
	}
	var session struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&session); err != nil {
		return "", nil, err
	}
	browser, err := pw.Chromium.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	return session.SessionID, browser, err
}

type attemptResult struct {
	Attempt     string  `json:"attempt"`
	OffsetS     *int    `json:"offset_s"`
	SecondsLeft float64 `json:"seconds_left_in_window"`
	WaitedS     float64 `json:"waited_s"`
	Landed      string  `json:"landed"`
	Accepted    bool    `json:"accepted"`
	Ms          int64   `json:"ms"`
}

// One login attempt in a fresh context (no cookies from earlier attempts). The code is made
// after the form has loaded, right before typing it: offset nil types a wrong code, otherwise
// the code for now + offset seconds.
func attemptOnce(browser playwright.Browser, secretKey []byte, label string, offset *int) (playwright.BrowserContext, attemptResult, error) {
	context, err := browser.NewContext()
	if err != nil {
		return nil, attemptResult{}, err
	}
	page, err := context.NewPage()
	if err != nil {
		return context, attemptResult{}, err
	}
	if _, err := page.Goto(site+"/totpChallenge/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
		return context, attemptResult{}, err
	}
	r := attemptResult{Attempt: label, OffsetS: offset}
	r.SecondsLeft, r.WaitedS = freshWindow()
	code := "000000"
	if offset != nil {
		code = totp(secretKey, now()+float64(*offset))
	}
	t := time.Now()
	for _, f := range [][2]string{{"#email", email}, {"#password", password}, {"#totpmfa", code}} {
		if err := page.Fill(f[0], f[1]); err != nil {
			return context, attemptResult{}, err
		}
	}
	if err := page.Click("input[type=submit]"); err != nil {
		return context, attemptResult{}, err
	}
	if err := page.WaitForURL(regexp.MustCompile(`/login(Success|Fail)/`), playwright.PageWaitForURLOptions{Timeout: playwright.Float(60000)}); err != nil {
		return context, attemptResult{}, err
	}
	u, _ := url.Parse(page.URL())
	r.Landed, r.Accepted, r.Ms = u.Path, u.Path == "/loginSuccess/", time.Since(t).Milliseconds()
	return context, r, nil
}

// The challenge page or the post-submit navigation occasionally hangs through a rotating proxy:
// one more try in a fresh context, with the code made again after the reload.
func attempt(browser playwright.Browser, secretKey []byte, label string, offset *int) (playwright.BrowserContext, attemptResult, error) {
	context, r, err := attemptOnce(browser, secretKey, label, offset)
	if err == nil || !errors.Is(err, playwright.ErrTimeout) {
		return context, r, err
	}
	if context != nil {
		context.Close()
	}
	return attemptOnce(browser, secretKey, label, offset)
}

type output struct {
	Site       string `json:"site"`
	SessionIDs struct {
		First  string `json:"first"`
		Second string `json:"second"`
	} `json:"session_ids"`
	Clocks       clocks          `json:"clocks"`
	Attempts     []attemptResult `json:"attempts"`
	StorageState struct {
		Cookies        int `json:"cookies"`
		SessionCookies int `json:"session_cookies"`
	} `json:"storage_state"`
	SecondSession struct {
		WithoutState signedIn `json:"without_state"`
		WithState    signedIn `json:"with_state"`
	} `json:"second_session"`
}

type clocks struct {
	ServerDateWithinRequest bool    `json:"server_date_within_request"`
	RequestS                float64 `json:"request_s"`
	BrowserMinusLocalS      float64 `json:"browser_minus_local_s"`
}

type signedIn struct {
	SignedIn bool `json:"signed_in"`
}

func intPtr(v int) *int { return &v }

func firstSession(pw *playwright.Playwright, secretKey []byte, out *output) (*playwright.StorageState, error) {
	id, browser, err := launch(pw)
	if err != nil {
		return nil, err
	}
	out.SessionIDs.First = id
	defer browser.Close()

	// What matters is your clock against the server's. The site's Date header (from its CDN
	// edge, 1 s resolution) should fall between our clock just before and just after the
	// request. The remote browser's clock is recorded for comparison only.
	page, err := browser.NewPage()
	if err != nil {
		return nil, err
	}
	t0 := now()
	resp, err := page.Goto(site+"/totpChallenge/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
	if err != nil {
		return nil, err
	}
	t1 := now()
	server, err := http.ParseTime(resp.Headers()["date"])
	if err != nil {
		return nil, err
	}
	browserNow, err := page.Evaluate("Date.now()")
	if err != nil {
		return nil, err
	}
	serverS := float64(server.Unix())
	out.Clocks = clocks{
		ServerDateWithinRequest: serverS >= math.Floor(t0)-1 && serverS <= t1+1,
		RequestS:                round1(t1 - t0),
		BrowserMinusLocalS:      round1(toFloat(browserNow)/1000 - now()),
	}
	page.Close()

	for _, tr := range []struct {
		label  string
		offset *int
	}{
		{"wrong code", nil},
		{"code from 2 windows ago (-60 s)", intPtr(-60)},
		{"code from the previous window (-30 s)", intPtr(-30)},
		{"code from the next window (+30 s)", intPtr(30)},
	} {
		context, r, err := attempt(browser, secretKey, tr.label, tr.offset)
		if err != nil {
			return nil, err
		}
		out.Attempts = append(out.Attempts, r)
		context.Close()
	}
	context, r, err := attempt(browser, secretKey, "current code", intPtr(0))
	if err != nil {
		return nil, err
	}
	out.Attempts = append(out.Attempts, r)
	state, err := context.StorageState()
	if err != nil {
		return nil, err
	}
	out.StorageState.Cookies = len(state.Cookies)
	for _, c := range state.Cookies {
		if c.Expires == -1 {
			out.StorageState.SessionCookies++
		}
	}
	return state, nil
}

// A new session (another browser): without the saved state, then with it.
func secondSession(pw *playwright.Playwright, state *playwright.StorageState, out *output) error {
	id, browser, err := launch(pw)
	if err != nil {
		return err
	}
	out.SessionIDs.Second = id
	defer browser.Close()
	for _, s := range []struct {
		result  *signedIn
		options playwright.BrowserNewContextOptions
	}{
		{&out.SecondSession.WithoutState, playwright.BrowserNewContextOptions{}},
		{&out.SecondSession.WithState, playwright.BrowserNewContextOptions{StorageState: state.ToOptionalStorageState()}},
	} {
		context, err := browser.NewContext(s.options)
		if err != nil {
			return err
		}
		page, err := context.NewPage()
		if err != nil {
			return err
		}
		if _, err := page.Goto(site+"/", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err != nil {
			return err
		}
		n, err := page.Locator("text=Sign Out").Count()
		if err != nil {
			return err
		}
		s.result.SignedIn = n > 0
		context.Close()
	}
	return nil
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func run() error {
	secretKey, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(secret, "="))
	if err != nil {
		return fmt.Errorf("TOTP secret: %w", err)
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()

	out := output{Site: site + "/totpChallenge/"}
	state, err := firstSession(pw, secretKey, &out)
	if err != nil {
		return err
	}
	if err := secondSession(pw, state, &out); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
	fmt.Print(buf.String())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
