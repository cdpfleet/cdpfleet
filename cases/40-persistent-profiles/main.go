// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/playwright-community/playwright-go"
)

const practice = "https://authenticationtest.com"
const site = "https://httpbin.org/html"

// The practice site publishes its test account and TOTP secret on the challenge page.
const email, password, secret = "totp@authenticationtest.com", "pa$$w0rd", "I65VU7K5ZQL7WB4E"

// Writes every kind of storage on httpbin.org, and reads it back.
const writeJS = `async () => {
  document.cookie = 'remember_me=alice; max-age=2592000; path=/';  // what "Remember me" sets
  document.cookie = 'session_only=yes; path=/';                    // no expiry: a session cookie
  localStorage.setItem('theme', 'dark');
  sessionStorage.setItem('draft', 'unsaved');
  await new Promise((resolve, reject) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const tx = open.result.transaction('auth', 'readwrite');
      tx.objectStore('auth').put({ email: 'alice@example.com', token: 'demo-token' }, 'user');
      tx.oncomplete = () => { open.result.close(); resolve(); };
      tx.onerror = () => reject(tx.error);
    };
  });
}`

const readJS = `async () => {
  const cookies = Object.fromEntries(document.cookie.split('; ').filter(Boolean).map((c) => c.split('=')));
  const idb = await new Promise((resolve) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains('auth')) { db.close(); resolve(null); return; }
      const get = db.transaction('auth').objectStore('auth').get('user');
      get.onsuccess = () => { db.close(); resolve(get.result ?? null); };
      get.onerror = () => { db.close(); resolve(null); };
    };
    open.onerror = () => resolve(null);
  });
  return {
    persistent_cookie: cookies.remember_me === 'alice',
    session_cookie: cookies.session_only === 'yes',
    local_storage: localStorage.getItem('theme') === 'dark',
    session_storage: sessionStorage.getItem('draft') === 'unsaved',
    indexeddb: idb?.email === 'alice@example.com',
  };
}`

var key = os.Getenv("CDPFLEET_API_KEY")
var name = func() string { b := make([]byte, 4); rand.Read(b); return "case40-go-" + hex.EncodeToString(b) }() // one profile per run

type Storage struct {
	PersistentCookie bool `json:"persistent_cookie"`
	SessionCookie    bool `json:"session_cookie"`
	LocalStorage     bool `json:"local_storage"`
	SessionStorage   bool `json:"session_storage"`
	IndexedDB        bool `json:"indexeddb"`
}

type ReadonlyEntry struct {
	Status           int    `json:"status"`
	Error            string `json:"error,omitempty"`
	PersistentCookie *bool  `json:"persistent_cookie,omitempty"`
	LocalStorage     *bool  `json:"local_storage,omitempty"`
	IndexedDB        *bool  `json:"indexeddb,omitempty"`
}

type Output struct {
	ProfileNamePrefix string `json:"profile_name_prefix"`
	Sessions          struct {
		A string `json:"a,omitempty"`
		B string `json:"b,omitempty"`
	} `json:"sessions"`
	First struct {
		LaunchMs              int64    `json:"launch_ms"`
		PracticeLogin         bool     `json:"practice_login"`
		PracticeCookieExpires []string `json:"practice_cookie_expires"`
		Wrote                 *Storage `json:"wrote,omitempty"`
	} `json:"first"`
	Saved struct {
		Version int   `json:"version"`
		Bytes   int64 `json:"bytes"`
	} `json:"saved"`
	Second struct {
		LaunchMs         int64    `json:"launch_ms"`
		Restored         *Storage `json:"restored,omitempty"`
		PracticeSignedIn bool     `json:"practice_signed_in"`
	} `json:"second"`
	NewContext   *Storage `json:"new_context"`
	SecondWriter struct {
		Status int     `json:"status"`
		Error  *string `json:"error"`
	} `json:"second_writer"`
	Readonly []ReadonlyEntry `json:"readonly"`
	After    struct {
		VersionAfterReadonly *int `json:"version_after_readonly,omitempty"`
		VersionAfterB        *int `json:"version_after_b,omitempty"`
		Deleted              bool `json:"deleted"`
	} `json:"after"`
}

type Profile struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Bytes   int64  `json:"bytes"`
	InUse   bool   `json:"in_use"`
}

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps), as in the TOTP login case.
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

func profileInfo() (*Profile, error) {
	req, _ := http.NewRequest("GET", "https://cdpfleet.com/v1/me/profiles", nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var list struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	for _, p := range list.Profiles {
		if p.Name == name {
			return &p, nil
		}
	}
	return nil, nil
}

// The node saves the profile after the session's browser has exited: wait for the new version.
func waitForVersion(version int) (*Profile, error) {
	for i := 0; i < 60; i++ {
		p, err := profileInfo()
		if err == nil && p != nil && p.Version >= version && !p.InUse {
			return p, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("profile %s never reached version %d", name, version)
}

type Started struct {
	Status int
	Body   map[string]any
	T      time.Time
}

// POST the launch with the profile (safe from goroutines: no Playwright here).
func start(profile any) Started {
	t := time.Now()
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true, "profile": profile})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return Started{Status: 0, Body: map[string]any{"error": err.Error()}, T: t}
	}
	defer res.Body.Close()
	var b map[string]any
	json.NewDecoder(res.Body).Decode(&b)
	return Started{Status: res.StatusCode, Body: b, T: t}
}

type Attached struct {
	Status    int
	Error     string
	SessionID string
	Browser   playwright.Browser
	Context   playwright.BrowserContext
	Page      playwright.Page
	LaunchMs  int64
}

// Connect to a launched session and open a page in the profile's context.
func attach(pw *playwright.Playwright, s Started) (*Attached, error) {
	if s.Status != 200 {
		e, _ := s.Body["error"].(string)
		return &Attached{Status: s.Status, Error: e}, nil
	}
	browser, err := pw.Chromium.Connect(s.Body["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return nil, err
	}
	context := browser.Contexts()[0] // the profile — a NewContext() would not be saved
	var page playwright.Page
	if pages := context.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = context.NewPage(); err != nil {
		browser.Close()
		return nil, err
	}
	return &Attached{Status: s.Status, SessionID: s.Body["sessionId"].(string), Browser: browser, Context: context, Page: page, LaunchMs: time.Since(s.T).Milliseconds()}, nil
}

// Proxies drop a tunnel now and then: up to two retries.
func gotoURL(page playwright.Page, u string) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if _, err = page.Goto(u, playwright.PageGotoOptions{Timeout: playwright.Float(60000)}); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return err
}

func storage(page playwright.Page) (*Storage, error) {
	if err := gotoURL(page, site); err != nil {
		return nil, err
	}
	v, err := page.Evaluate(readJS)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(v)
	var s Storage
	return &s, json.Unmarshal(raw, &s)
}

func signedIntoPractice(page playwright.Page) (bool, error) {
	if err := gotoURL(page, practice+"/"); err != nil {
		return false, err
	}
	n, err := page.Locator("text=Sign Out").Count()
	return n > 0, err
}

func sessionA(pw *playwright.Playwright, out *Output, secretKey []byte) error {
	a, err := attach(pw, start(name))
	if err != nil {
		return err
	}
	if a.Browser == nil {
		return fmt.Errorf("launch A %d %s", a.Status, a.Error)
	}
	defer a.Browser.Close() // ends the session; the node saves the profile once the browser has exited
	out.Sessions.A = a.SessionID
	out.First.LaunchMs = a.LaunchMs
	if err := gotoURL(a.Page, practice+"/totpChallenge/"); err != nil {
		return err
	}
	if 30-math.Mod(now(), 30) < 5 {
		time.Sleep(5200 * time.Millisecond) // fresh TOTP window
	}
	for sel, v := range map[string]string{"#email": email, "#password": password, "#totpmfa": totp(secretKey, now())} {
		if err := a.Page.Fill(sel, v); err != nil {
			return err
		}
	}
	if err := a.Page.Click("input[type=submit]"); err != nil {
		return err
	}
	// playwright-go 0.6000's WaitForURL panics in a context the client didn't create (it reads
	// the context's options, which are nil for contexts()[0]), so poll the URL instead.
	landedRe := regexp.MustCompile(`/login(Success|Fail)/`)
	for deadline := time.Now().Add(60 * time.Second); !landedRe.MatchString(a.Page.URL()); time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return fmt.Errorf("login result page never loaded (at %s)", a.Page.URL())
		}
	}
	landed, _ := url.Parse(a.Page.URL())
	out.First.PracticeLogin = landed.Path == "/loginSuccess/"
	cookies, err := a.Context.Cookies(practice)
	if err != nil {
		return err
	}
	out.First.PracticeCookieExpires = []string{}
	for _, c := range cookies {
		if c.Expires == -1 {
			out.First.PracticeCookieExpires = append(out.First.PracticeCookieExpires, "session")
		} else {
			out.First.PracticeCookieExpires = append(out.First.PracticeCookieExpires, "persistent")
		}
	}
	if err := gotoURL(a.Page, site); err != nil {
		return err
	}
	if _, err := a.Page.Evaluate(writeJS); err != nil {
		return err
	}
	out.First.Wrote, err = storage(a.Page)
	return err
}

func sessionB(pw *playwright.Playwright, out *Output) error {
	b, err := attach(pw, start(name))
	if err != nil {
		return err
	}
	if b.Browser == nil {
		return fmt.Errorf("launch B %d %s", b.Status, b.Error)
	}
	defer b.Browser.Close()
	out.Sessions.B = b.SessionID
	out.Second.LaunchMs = b.LaunchMs
	if out.Second.Restored, err = storage(b.Page); err != nil {
		return err
	}
	if out.Second.PracticeSignedIn, err = signedIntoPractice(b.Page); err != nil {
		return err
	}

	// The trap: a new context in the same session starts empty and is never saved.
	scratch, err := b.Browser.NewContext()
	if err != nil {
		return err
	}
	page, err := scratch.NewPage()
	if err != nil {
		return err
	}
	if out.NewContext, err = storage(page); err != nil {
		return err
	}
	scratch.Close()

	// While B holds the profile: a second normal launch is refused, read-only ones work.
	w, err := attach(pw, start(name))
	if err != nil {
		return err
	}
	out.SecondWriter.Status = w.Status
	if w.Error != "" {
		out.SecondWriter.Error = &w.Error
	}
	if w.Browser != nil {
		w.Browser.Close()
	}
	// Launch the readers in parallel (HTTP only); Playwright is driven from this goroutine.
	launches := make([]Started, 3)
	done := make(chan struct{})
	for i := range launches {
		go func(i int) { launches[i] = start(map[string]string{"id": name, "mode": "readonly"}); done <- struct{}{} }(i)
	}
	for range launches {
		<-done
	}
	out.Readonly = []ReadonlyEntry{}
	for _, s := range launches {
		r, err := attach(pw, s)
		if err != nil {
			return err
		}
		if r.Browser == nil {
			out.Readonly = append(out.Readonly, ReadonlyEntry{Status: r.Status, Error: r.Error})
			continue
		}
		st, err := storage(r.Page)
		r.Browser.Close()
		if err != nil {
			return err
		}
		out.Readonly = append(out.Readonly, ReadonlyEntry{Status: r.Status, PersistentCookie: &st.PersistentCookie, LocalStorage: &st.LocalStorage, IndexedDB: &st.IndexedDB})
	}
	if _, err := b.Page.Evaluate("() => document.title"); err != nil { // keep B active (idle sessions end after 60 s on shared threads)
		return err
	}
	if p, err := profileInfo(); err == nil && p != nil {
		out.After.VersionAfterReadonly = &p.Version
	}
	return nil
}

func run(out *Output) error {
	secretKey, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return fmt.Errorf("TOTP secret: %w", err)
	}
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	defer func() {
		// Delete the profile once no session holds it.
		for i := 0; i < 60; i++ {
			if p, err := profileInfo(); err != nil || p == nil || !p.InUse {
				break
			}
			time.Sleep(2 * time.Second)
		}
		req, _ := http.NewRequest("DELETE", "https://cdpfleet.com/v1/me/profiles/"+name, nil)
		req.Header.Set("x-api-key", key)
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
			p, perr := profileInfo()
			out.After.Deleted = res.StatusCode == 200 && perr == nil && p == nil
		}
	}()
	if err := sessionA(pw, out, secretKey); err != nil {
		return err
	}
	saved, err := waitForVersion(1)
	if err != nil {
		return err
	}
	out.Saved.Version, out.Saved.Bytes = saved.Version, saved.Bytes
	if err := sessionB(pw, out); err != nil { // B's browser is closed when it returns
		return err
	}
	after, err := waitForVersion(2)
	if err != nil {
		return err
	}
	out.After.VersionAfterB = &after.Version
	return nil
}

func main() {
	out := Output{ProfileNamePrefix: "case40-"}
	if err := run(&out); err != nil {
		log.Fatal(err)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}
