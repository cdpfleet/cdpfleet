// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const (
	site     = "https://practicesoftwaretesting.com" // Toolshop: a public demo shop built for test automation
	api      = "https://api.practicesoftwaretesting.com"
	email    = "customer@practicesoftwaretesting.com" // the site's published demo account
	password = "welcome01"
)

// Fetches every page of the API from inside the page with the token: one by one, then all at once.
// (No JS template literals: Go raw strings can't hold backticks.)
const fetchAll = `async ({ api, token }) => {
  const get = (n) => fetch(api + '/invoices?page=' + n, { headers: { authorization: 'Bearer ' + token } }).then(async (r) => ({ status: r.status, text: await r.text() }));
  const meta = JSON.parse((await get(1)).text);
  const numbers = Array.from({ length: meta.last_page }, (_, i) => i + 1);
  let t = performance.now();
  const pages = [];
  for (const n of numbers) pages.push(await get(n));
  const sequential = (performance.now() - t) / 1000;
  t = performance.now();
  await Promise.all(numbers.map(get));
  const parallel = (performance.now() - t) / 1000;
  const noToken = await fetch(api + '/invoices?page=1').then((r) => r.status);
  const items = pages.flatMap((p) => JSON.parse(p.text).data);
  return JSON.stringify({
    sequential, parallel, total: meta.total, perPage: meta.per_page, lastPage: meta.last_page,
    requests: pages.length, bytes: pages.reduce((s, p) => s + p.text.length, 0), statuses: [...new Set(pages.map((p) => p.status))],
    invoices: items.map((i) => i.invoice_number), fields: Object.keys(items[0]).length, noToken,
  });
}`

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
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
		wait, _ := strconv.Atoi(res.Header.Get("retry-after"))
		if wait == 0 {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func readTable(page playwright.Page) []string {
	cells, err := page.EvalOnSelectorAll("table tbody tr td:first-child", "tds => tds.map((td) => td.textContent.trim())")
	must(err)
	var out []string
	for _, c := range cells.([]any) {
		out = append(out, c.(string))
	}
	return out
}

func main() {
	session, err := launch(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new", "record": false})
	must(err)
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	must(err)
	defer pw.Stop()
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	must(err)
	defer browser.Close()

	page, err := browser.NewPage()
	must(err)
	// The app can take 30 s to render through a rotating residential exit: wait up to 90 s.
	page.SetDefaultTimeout(90000)

	// 1. Log in like a person; the site stores its token and sends it with every API call.
	_, err = page.Goto(site + "/auth/login")
	must(err)
	must(page.Fill("#email", email))
	must(page.Fill("#password", password))
	must(page.Click("[type=submit]"))
	must(page.WaitForURL("**/account"))

	// 2. Find the token twice: in localStorage, and in the header of the page's own API request.
	storedAny, err := page.Evaluate("() => localStorage.getItem('auth-token')")
	must(err)
	stored, _ := storedAny.(string)
	var uiCalls, counting int32 = 0, 1
	page.OnRequest(func(r playwright.Request) {
		if atomic.LoadInt32(&counting) == 1 && strings.HasPrefix(r.URL(), api+"/invoices") {
			atomic.AddInt32(&uiCalls, 1)
		}
	})
	firstCall, err := page.ExpectRequest(regexp.MustCompile(`^`+regexp.QuoteMeta(api+"/invoices")), func() error {
		_, err := page.Goto(site + "/account/invoices")
		return err
	})
	must(err)
	headers, err := firstCall.AllHeaders()
	must(err)
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(stored, ".")[1])
	must(err)
	var claims struct {
		Exp int64 `json:"exp"`
		Iat int64 `json:"iat"`
	}
	must(json.Unmarshal(raw, &claims))

	// 3. The page's way: click through the pager, reading the table each time.
	_, err = page.WaitForSelector("table tbody tr")
	must(err)
	t0 := time.Now()
	viaUi := readTable(page)
	lastText, err := page.Locator(`button[aria-label^="Page-"]`).Last().InnerText()
	must(err)
	lastPage, _ := strconv.Atoi(strings.TrimSpace(lastText))
	for n := 2; n <= lastPage; n++ {
		_, err := page.ExpectResponse(fmt.Sprintf("%s/invoices?page=%d", api, n), func() error {
			return page.Click(`[data-test="pagination-next"]`)
		})
		must(err)
		_, err = page.WaitForFunction("first => document.querySelector('table tbody tr td')?.textContent.trim() !== first", viaUi[len(viaUi)-15])
		must(err)
		viaUi = append(viaUi, readTable(page)...)
	}
	uiSeconds := time.Since(t0).Seconds()
	atomic.StoreInt32(&counting, 0)
	columns, err := page.Locator("table thead th").Count()
	must(err)

	// 4. The API's way: the same token on fetch() from inside the page — the browser makes the
	//    requests, through the session's proxy.
	res, err := page.Evaluate(fetchAll, map[string]any{"api": api, "token": stored})
	must(err)
	var viaApi struct {
		Sequential float64  `json:"sequential"`
		Parallel   float64  `json:"parallel"`
		Total      int      `json:"total"`
		PerPage    int      `json:"perPage"`
		LastPage   int      `json:"lastPage"`
		Requests   int      `json:"requests"`
		Bytes      int      `json:"bytes"`
		Statuses   []int    `json:"statuses"`
		Invoices   []string `json:"invoices"`
		Fields     int      `json:"fields"`
		NoToken    int      `json:"noToken"`
	}
	must(json.Unmarshal([]byte(res.(string)), &viaApi))
	a := append([]string{}, viaUi...)
	b := append([]string{}, viaApi.Invoices...)
	sort.Strings(a)
	sort.Strings(b)

	inApi := map[string]bool{}
	for _, n := range viaApi.Invoices {
		inApi[n] = true
	}
	seen := map[string]bool{}
	missing := 0
	for _, n := range viaUi {
		if !inApi[n] && !seen[n] {
			missing++
		}
		seen[n] = true
	}
	var inStorage any
	if stored != "" {
		inStorage = "auth-token"
	}
	out, _ := json.Marshal(map[string]any{
		"token": map[string]any{
			"in_local_storage":    inStorage,
			"header_matches":      headers["authorization"] == "Bearer "+stored,
			"lifetime_seconds":    claims.Exp - claims.Iat,
			"seconds_left_at_end": claims.Exp - time.Now().Unix(),
		},
		"api": map[string]any{"total": viaApi.Total, "per_page": viaApi.PerPage, "last_page": viaApi.LastPage, "without_token": viaApi.NoToken},
		"ui":  map[string]any{"pages": lastPage, "invoices": len(viaUi), "invoice_calls": atomic.LoadInt32(&uiCalls), "columns": columns, "seconds": uiSeconds},
		"direct": map[string]any{
			"requests": viaApi.Requests, "statuses": viaApi.Statuses, "invoices": len(viaApi.Invoices), "fields_per_invoice": viaApi.Fields,
			"json_bytes": viaApi.Bytes, "seconds_one_by_one": viaApi.Sequential, "seconds_all_at_once": viaApi.Parallel,
		},
		"same_invoices":       strings.Join(a, ",") == strings.Join(b, ","),
		"ui_missing_from_api": missing,
	})
	fmt.Println(string(out))
}
