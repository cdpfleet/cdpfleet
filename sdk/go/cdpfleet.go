// Package cdpfleet launches remote browsers on cdpfleet and connects your own
// Playwright (playwright-go) to them. Spec shared by every language: ../SPEC.md
package cdpfleet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

// Version is the SDK version sent in the User-Agent header.
const Version = "0.1.0"

const userAgent = "cdpfleet-sdk-go/" + Version

var quotaCodes = map[string]bool{"daily_quota_exhausted": true, "hourly_quota_exhausted": true}

// Options is the launch body, passed to the API untouched (proxy, headless, cdp,
// keep_alive, record, proxy_rules, Camoufox options, ...).
type Options map[string]any

// Error is a non-2xx answer from the cdpfleet API (Status 0 = network failure).
type Error struct {
	Status  int
	Code    string // the body's "error" field
	Message string
	Body    any // the parsed JSON body
}

func (e *Error) Error() string { return e.Message }

func newError(status int, body any, message string) *Error {
	code := ""
	if m, ok := body.(map[string]any); ok {
		if s, ok := m["error"].(string); ok {
			code = s
		}
	}
	if message == "" {
		message = strings.TrimSpace(fmt.Sprintf("cdpfleet: %d %s", status, code))
	}
	return &Error{Status: status, Code: code, Message: message, Body: body}
}

// readJSON parses a response body; a non-JSON body becomes {"error": first 200 chars}.
func readJSON(res *http.Response) (any, error) {
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		text := string(data)
		if len(text) > 200 {
			text = text[:200]
		}
		return map[string]any{"error": text}, nil
	}
	return v, nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// Client talks to the cdpfleet starter (launches) and account API.
type Client struct {
	APIKey     string
	StarterURL string
	APIURL     string
	MaxWait    time.Duration
	HTTPClient *http.Client
}

// ClientOption configures New.
type ClientOption func(*Client)

// WithAPIKey sets the API key (default: env CDPFLEET_API_KEY).
func WithAPIKey(key string) ClientOption { return func(c *Client) { c.APIKey = key } }

// WithStarterURL sets the launch endpoint base (default https://starter.cdpfleet.com).
func WithStarterURL(u string) ClientOption { return func(c *Client) { c.StarterURL = u } }

// WithAPIURL sets the account API base (default https://cdpfleet.com).
func WithAPIURL(u string) ClientOption { return func(c *Client) { c.APIURL = u } }

// WithMaxWait sets how long Launch keeps retrying backpressure (default 120 s).
func WithMaxWait(d time.Duration) ClientOption { return func(c *Client) { c.MaxWait = d } }

// New creates a client; it fails if no API key is given or set in CDPFLEET_API_KEY.
func New(opts ...ClientOption) (*Client, error) {
	c := &Client{
		APIKey:     os.Getenv("CDPFLEET_API_KEY"),
		StarterURL: "https://starter.cdpfleet.com",
		APIURL:     "https://cdpfleet.com",
		MaxWait:    120 * time.Second,
		HTTPClient: &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	if c.APIKey == "" {
		return nil, errors.New("cdpfleet: no API key (pass WithAPIKey or set CDPFLEET_API_KEY)")
	}
	c.StarterURL = strings.TrimSuffix(c.StarterURL, "/")
	c.APIURL = strings.TrimSuffix(c.APIURL, "/")
	return c, nil
}

func (c *Client) do(ctx context.Context, method, endpoint string, body any, timeout time.Duration) (*http.Response, any, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("user-agent", userAgent)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	parsed, err := readJSON(res)
	if err != nil {
		return nil, nil, err
	}
	return res, parsed, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func backoff(n int) time.Duration {
	s := 1 << n
	if n > 4 || s > 10 {
		s = 10
	}
	return time.Duration(s) * time.Second
}

// Launch starts a browser; it retries fleet backpressure (503) and rate limits (429) as the API asks.
func (c *Client) Launch(browser string, options Options) (*Session, error) {
	return c.LaunchContext(context.Background(), browser, options)
}

// LaunchContext is Launch with a context that can cancel the retries.
func (c *Client) LaunchContext(ctx context.Context, browser string, options Options) (*Session, error) {
	if options == nil {
		options = Options{}
	}
	deadline := time.Now().Add(c.MaxWait)
	for attempt := 1; ; attempt++ {
		res, body, err := c.do(ctx, http.MethodPost, c.StarterURL+"/"+browser+"/session", options, 60*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if time.Now().After(deadline) {
				return nil, newError(0, map[string]any{"error": "network"}, "cdpfleet: launch failed: "+err.Error())
			}
			if err := sleepCtx(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		if res.StatusCode == http.StatusOK {
			return newSession(c, asMap(body), options), nil
		}
		code, _ := asMap(body)["error"].(string)
		retryable := res.StatusCode == 503 || (res.StatusCode == 429 && !quotaCodes[code])
		if !retryable || time.Now().After(deadline) {
			return nil, newError(res.StatusCode, body, "")
		}
		wait := backoff(attempt - 1)
		if after, err := strconv.ParseFloat(res.Header.Get("retry-after"), 64); err == nil && after > 0 {
			wait = time.Duration(after * float64(time.Second))
		}
		wait += time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
		if err := sleepCtx(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) request(method, path string, body any) (any, error) {
	res, parsed, err := c.do(context.Background(), method, c.APIURL+path, body, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, newError(res.StatusCode, parsed, "")
	}
	return parsed, nil
}

func (c *Client) get(path string) (map[string]any, error) {
	v, err := c.request(http.MethodGet, path, nil)
	return asMap(v), err
}

// Me returns GET /v1/me.
func (c *Client) Me() (map[string]any, error) { return c.get("/v1/me") }

// Usage returns GET /v1/me/usage.
func (c *Client) Usage() (map[string]any, error) { return c.get("/v1/me/usage") }

// Threads returns GET /v1/me/threads (running sessions).
func (c *Client) Threads() (map[string]any, error) { return c.get("/v1/me/threads") }

// Sessions returns GET /v1/me/sessions; days or limit <= 0 are left out.
func (c *Client) Sessions(days, limit int) (map[string]any, error) {
	q := url.Values{}
	if days > 0 {
		q.Set("days", strconv.Itoa(days))
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/v1/me/sessions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return c.get(path)
}

// Recordings returns GET /v1/me/recordings.
func (c *Client) Recordings() (map[string]any, error) { return c.get("/v1/me/recordings") }

// RecordingURL returns a download URL for a recording.
func (c *Client) RecordingURL(id string) (string, error) {
	m, err := c.get("/v1/me/recordings/" + url.PathEscape(id) + "/url")
	if err != nil {
		return "", err
	}
	s, _ := m["url"].(string)
	return s, nil
}

// DeleteRecording deletes a recording.
func (c *Client) DeleteRecording(id string) (map[string]any, error) {
	v, err := c.request(http.MethodDelete, "/v1/me/recordings/"+url.PathEscape(id), nil)
	return asMap(v), err
}

// ProxyBalance returns GET /v1/me/proxy-balance.
func (c *Client) ProxyBalance() (map[string]any, error) { return c.get("/v1/me/proxy-balance") }

// EndSession ends a session at once (404 = already ended, not an error).
func (c *Client) EndSession(sessionID string) (map[string]any, error) {
	v, err := c.request(http.MethodDelete, "/v1/me/threads/"+url.PathEscape(sessionID), nil)
	var e *Error
	if errors.As(err, &e) && e.Status == 404 {
		return nil, nil
	}
	return asMap(v), err
}

// Session is a launched remote browser. Close it with defer session.Close().
type Session struct {
	ID            string
	WsURL         string
	CdpURL        string // empty unless launched with cdp: true
	BrowserName   string
	ResourceClass string
	Weight        float64
	ExpiresAt     string
	KeepAlive     bool
	Raw           map[string]any

	client   *Client
	mu       sync.Mutex
	browsers []playwright.Browser
	closed   bool
}

func newSession(c *Client, raw map[string]any, options Options) *Session {
	str := func(k string) string { s, _ := raw[k].(string); return s }
	weight, _ := raw["weight"].(float64)
	expires := str("expires_at")
	if expires == "" && raw["expires_at"] != nil {
		expires = fmt.Sprint(raw["expires_at"])
	}
	keepAlive, _ := options["keep_alive"].(bool)
	return &Session{
		ID: str("sessionId"), WsURL: str("wsUrl"), CdpURL: str("cdpUrl"), BrowserName: str("browser"),
		ResourceClass: str("resource_class"), Weight: weight, ExpiresAt: expires, KeepAlive: keepAlive,
		Raw: raw, client: c,
	}
}

func (s *Session) withKey(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[k] = v
	}
	out["x-api-key"] = s.client.APIKey
	return out
}

// Right after a disconnect the router may still count the old connection (409
// session_already_connected): a keep_alive re-attach retries that for up to 10 s.
func (s *Session) attach(open func() (playwright.Browser, error)) (playwright.Browser, error) {
	for i := 0; ; i++ {
		b, err := open()
		if err == nil {
			s.mu.Lock()
			s.browsers = append(s.browsers, b)
			s.mu.Unlock()
			return b, nil
		}
		if i >= 10 || !strings.Contains(err.Error(), "session_already_connected") {
			return nil, err
		}
		time.Sleep(time.Second)
	}
}

// Connect attaches a Playwright browser type (pw.Chromium, pw.Firefox, pw.WebKit) over the
// Playwright protocol. Call again after a disconnect to re-attach to a keep_alive session.
func (s *Session) Connect(bt playwright.BrowserType, options ...playwright.BrowserTypeConnectOptions) (playwright.Browser, error) {
	var o playwright.BrowserTypeConnectOptions
	if len(options) > 0 {
		o = options[0]
	}
	o.Headers = s.withKey(o.Headers)
	return s.attach(func() (playwright.Browser, error) { return bt.Connect(s.WsURL, o) })
}

// ConnectOverCDP attaches over CDP (launch with cdp: true) via Playwright's ConnectOverCDP.
func (s *Session) ConnectOverCDP(bt playwright.BrowserType, options ...playwright.BrowserTypeConnectOverCDPOptions) (playwright.Browser, error) {
	if s.CdpURL == "" {
		return nil, errors.New("cdpfleet: this session has no cdpUrl (launch it with cdp: true)")
	}
	var o playwright.BrowserTypeConnectOverCDPOptions
	if len(options) > 0 {
		o = options[0]
	}
	o.Headers = s.withKey(o.Headers)
	return s.attach(func() (playwright.Browser, error) { return bt.ConnectOverCDP(s.CdpURL, o) })
}

func (s *Session) router(path string, body map[string]any) (map[string]any, error) {
	u, err := url.Parse(s.WsURL)
	if err != nil {
		return nil, err
	}
	res, parsed, err := s.client.do(context.Background(), http.MethodPost, "https://"+u.Host+path, body, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, newError(res.StatusCode, parsed, "")
	}
	return asMap(parsed), nil
}

// LiveLink returns a single-use live-view link ("view" or "control"; "" = "view").
// It works before your code connects.
func (s *Session) LiveLink(mode string) (map[string]any, error) {
	if mode == "" {
		mode = "view"
	}
	return s.router("/admin/session/live", map[string]any{"session_id": s.ID, "mode": mode})
}

// SwapProxy swaps the proxy of a session launched with proxy_updatable. A swap replaces
// the whole route, so pass the proxy rules again (nil = none).
func (s *Session) SwapProxy(proxy any, proxyRules any) (map[string]any, error) {
	body := map[string]any{"session_id": s.ID, "proxy": proxy}
	if proxyRules != nil {
		body["proxy_rules"] = proxyRules
	}
	return s.router("/admin/session/proxy", body)
}

// End ends the session now (keep_alive sessions otherwise live until inactivity_timeout).
func (s *Session) End() error {
	_, err := s.client.EndSession(s.ID)
	return err
}

// Close closes every browser this session connected (ignoring errors) and ends
// keep_alive sessions too. Safe to call twice.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	browsers := s.browsers
	s.browsers = nil
	s.mu.Unlock()
	for _, b := range browsers {
		_ = b.Close()
	}
	if s.KeepAlive {
		return s.End()
	}
	return nil
}
