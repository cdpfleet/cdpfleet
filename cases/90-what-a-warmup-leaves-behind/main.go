// Go 1.22, github.com/playwright-community/playwright-go v0.6000.0
// env: CDPFLEET_API_KEY (a paid plan: jobs and profiles aren't on Community), PROXY_URL_US (a sticky US exit)
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

const api = "https://cdpfleet.com"
const stopAfterS = 150
const day = 86400.0

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL_US")
var name = "case90-go-" + randHex(4) // a profile of its own for each run
// Three ordinary public sites: an encyclopedia, a scraping sandbox, a news front page.
var sites = []string{"https://en.wikipedia.org/wiki/Main_Page", "https://books.toscrape.com/", "https://www.theguardian.com/us"}
var warmup = M{
	"url":      sites[0],
	"sites":    sites[1:],
	"browser":  "camoufox",
	"profile":  name,
	"duration": 600, // a safety net: the program stops the job itself
	// No typing: the warmup would otherwise type into search boxes now and then.
	"behavior": M{"scroll": true, "mouse": true, "typing": false, "dwell_s": []int{4, 10}, "pages_per_site": []int{2, 4}},
}
var ended = map[string]bool{"done": true, "failed": true, "stopped": true, "interrupted": true}
var secondLevel = regexp.MustCompile(`^(co|com|org|net|ac|gov)\.[a-z]{2}$`)

const storageJS = "async () => ({ local_storage: localStorage.length, indexeddb: (await indexedDB.databases()).length })"

type M = map[string]any

// A cookie as the API and Playwright both describe it.
type ck struct {
	Name, Domain, Path, SameSite string
	Expires                      float64
	HttpOnly                     bool
}

func (c ck) key() string { return c.Name + "|" + c.Domain + "|" + c.Path }

func randHex(n int) string { b := make([]byte, n); rand.Read(b); return hex.EncodeToString(b) }

func send(method, u string, body any) (int, []byte, http.Header) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	req.Header.Set("x-api-key", key)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res, err := (&http.Client{Timeout: 120 * time.Second}).Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b, res.Header
}

func call(method, path string, body any) (int, M) {
	status, b, _ := send(method, api+path, body)
	var m M
	json.Unmarshal(b, &m)
	return status, m
}

func str(v any) string { s, _ := v.(string); return s }
func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
func obj(v any) M     { m, _ := v.(M); return m }
func arr(v any) []any { a, _ := v.([]any); return a }
func nowMs() float64  { return float64(time.Now().UnixMilli()) }
func secs(ms float64) float64 { return math.Round(ms/100) / 10 }
func ms(iso any) float64 { // an API timestamp
	t, _ := time.Parse(time.RFC3339Nano, str(iso))
	return float64(t.UnixMilli())
}

func profileInfo() M {
	_, r := call("GET", "/v1/me/profiles", nil)
	for _, p := range arr(r["profiles"]) {
		if str(obj(p)["name"]) == name {
			return obj(p)
		}
	}
	return nil
}

// The site a cookie or a host belongs to: its last two labels (three under co.uk and the like).
func site(host string) string {
	l := strings.Split(strings.TrimPrefix(host, "."), ".")
	n := 2
	if len(l) >= 2 && secondLevel.MatchString(l[len(l)-2]+"."+l[len(l)-1]) {
		n = 3
	}
	if n > len(l) {
		n = len(l)
	}
	return strings.Join(l[len(l)-n:], ".")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Cookies by site, first- vs third-party (first = a site the warmup visited as a page), and how long they last.
func summarize(cookies []ck, firstParty []string, now float64) M {
	bySite := map[string]int{}
	for _, c := range cookies {
		bySite[site(c.Domain)]++
	}
	type kv struct {
		s string
		n int
	}
	third := []kv{}
	for s, n := range bySite {
		if !contains(firstParty, s) {
			third = append(third, kv{s, n})
		}
	}
	sort.Slice(third, func(i, j int) bool {
		if third[i].n != third[j].n {
			return third[i].n > third[j].n
		}
		return third[i].s < third[j].s
	})
	top := []string{}
	for i := 0; i < len(third) && i < 5; i++ {
		top = append(top, fmt.Sprintf("%s %d", third[i].s, third[i].n))
	}
	left := []float64{}
	first, httpOnly, none := 0, 0, 0
	keys := map[string]bool{}
	for _, c := range cookies {
		if c.Expires > 0 {
			left = append(left, (c.Expires-now)/day)
		}
		if contains(firstParty, site(c.Domain)) {
			first++
		}
		if c.HttpOnly {
			httpOnly++
		}
		if c.SameSite == "None" {
			none++
		}
		keys[c.key()] = true
	}
	byFirst := M{}
	for _, s := range firstParty {
		byFirst[s] = bySite[s]
	}
	count := func(f func(float64) bool) int {
		n := 0
		for _, d := range left {
			if f(d) {
				n++
			}
		}
		return n
	}
	var maxDays any
	if len(left) > 0 {
		m := left[0]
		for _, d := range left {
			m = math.Max(m, d)
		}
		maxDays = math.Round(m)
	}
	return M{
		"total": len(cookies), "sites": len(bySite), "first_party": first, "third_party": len(cookies) - first,
		"third_party_sites": len(third), "by_first_party_site": byFirst, "top_third_party": top,
		"session": len(cookies) - len(left), "http_only": httpOnly, "same_site_none": none,
		"expiry": M{
			"under_1_day":    count(func(d float64) bool { return d < 1 }),
			"under_30_days":  count(func(d float64) bool { return d >= 1 && d < 30 }),
			"under_1_year":   count(func(d float64) bool { return d >= 30 && d < 365 }),
			"a_year_or_more": count(func(d float64) bool { return d >= 365 }),
			"max_days":       maxDays,
		},
		"same_key_twice": len(cookies) - len(keys), // e.g. Firefox's per-site (partitioned) copies
	}
}

func launch(pw *playwright.Playwright, profile any) (string, playwright.Browser) {
	body := M{"proxy": proxy, "headless": true}
	if profile != nil {
		body["profile"] = profile
	}
	for attempt := 1; ; attempt++ {
		status, b, h := send("POST", "https://starter.cdpfleet.com/camoufox/session", body)
		if status == 200 || status == 201 {
			var s M
			json.Unmarshal(b, &s)
			browser, err := pw.Firefox.Connect(str(s["wsUrl"]), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
			if err != nil {
				panic(err)
			}
			return str(s["sessionId"]), browser
		}
		if status != 503 || attempt == 5 {
			panic(fmt.Sprintf("launch %d %s", status, b))
		}
		wait, err := strconv.Atoi(h.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

type look struct {
	sessionID      string
	cookiesAtStart []ck
	origins        []M
	seconds        float64
}

// What one later session sees: the cookies it starts with, then for each warmed site one request for its
// /robots.txt — the Cookie header that first request carries is what the site gets from this browser, and
// the plain-text page lets us read the origin's localStorage and IndexedDB with none of the site's scripts run.
func inspect(pw *playwright.Playwright, profile any) look {
	sessionID, browser := launch(pw, profile)
	defer browser.Close()
	t := nowMs()
	// With a profile, its context is browser.Contexts()[0]; without one, a fresh context.
	var context playwright.BrowserContext
	var err error
	if profile != nil {
		context = browser.Contexts()[0]
	} else if context, err = browser.NewContext(); err != nil {
		panic(err)
	}
	var page playwright.Page
	if pages := context.Pages(); len(pages) > 0 {
		page = pages[0]
	} else if page, err = context.NewPage(); err != nil {
		panic(err)
	}
	raw, err := context.Cookies()
	if err != nil {
		panic(err)
	}
	atStart := []ck{}
	for _, c := range raw {
		ss := ""
		if c.SameSite != nil {
			ss = string(*c.SameSite)
		}
		atStart = append(atStart, ck{c.Name, c.Domain, c.Path, ss, c.Expires, c.HttpOnly})
	}
	origins := []M{}
	for _, u := range sites {
		pu, _ := url.Parse(u)
		origin := pu.Scheme + "://" + pu.Host
		var res playwright.Response
		var err error
		for attempt := 1; attempt <= 3 && res == nil; attempt++ {
			res, err = page.Goto(origin+"/robots.txt", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
			if err != nil {
				if attempt == 3 {
					panic(err)
				}
				res = nil
				time.Sleep(2 * time.Second)
			}
		}
		headers, _ := res.Request().AllHeaders()
		names := []string{}
		if h := headers["cookie"]; h != "" {
			for _, c := range strings.Split(h, "; ") {
				n, _, _ := strings.Cut(c, "=")
				names = append(names, n)
			}
		}
		first8 := names
		if len(first8) > 8 {
			first8 = first8[:8]
		}
		v, err := page.Evaluate(storageJS)
		if err != nil {
			panic(err)
		}
		storage := obj(v)
		origins = append(origins, M{
			"origin": origin, "status": res.Status(), "cookie_header_cookies": len(names), "cookie_header_names": first8,
			"local_storage": int(num(storage["local_storage"])), "indexeddb": int(num(storage["indexeddb"])),
		})
		time.Sleep(3 * time.Second)
	}
	return look{sessionID, atStart, origins, secs(nowMs() - t)}
}

func main() {
	shown := M{"stop_after_s": stopAfterS}
	for k, v := range warmup {
		shown[k] = v
	}
	shown["profile"] = "case90-go-…"
	out := M{"measured_at": time.Now().UTC().Format("2006-01-02"), "sites": sites, "warmup": shown}
	id := ""
	defer func() {
		// Stop the job if something failed early, then delete the profile once nothing holds it.
		if id != "" && out["job"] == nil {
			call("DELETE", "/v1/jobs/"+id, nil)
		}
		for i := 0; i < 60; i++ {
			if p := profileInfo(); p == nil || p["in_use"] != true {
				break
			}
			time.Sleep(2 * time.Second)
		}
		status, del := call("DELETE", "/v1/me/profiles/"+name, nil)
		out["cleanup"] = M{"delete_status": status, "deleted": status == 200 && del["deleted"] == true && profileInfo() == nil}
		if r := recover(); r != nil {
			panic(r)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		enc.Encode(out)
	}()

	// 1. Start the warmup into a new profile, on a sticky US exit.
	t0 := nowMs()
	body := M{"launch": M{"headless": true, "proxy": proxy}}
	for k, v := range warmup {
		body[k] = v
	}
	status, start := call("POST", "/v1/functions/warmup", body)
	if status != 202 {
		panic(fmt.Sprintf("warmup %d %v", status, start))
	}
	id = str(start["job_id"])
	shown["start_status"] = status

	// 2. Follow its progress; stop it after stopAfterS seconds.
	samples := []M{}
	var job M
	var stop M
	stopAt := 0.0
	for {
		time.Sleep(5 * time.Second)
		_, job = call("GET", "/v1/jobs/"+id, nil)
		p := M{}
		for k, v := range obj(job["progress"]) {
			p[k] = v
		}
		p["s"] = math.Round((nowMs() - t0) / 1000)
		samples = append(samples, p)
		if ended[str(job["status"])] {
			break
		}
		if stop == nil && nowMs()-t0 >= stopAfterS*1000 {
			st, r := call("DELETE", "/v1/jobs/"+id, nil)
			stop = M{"after_seconds": secs(nowMs() - t0), "status": st, "reply": r["status"]}
			stopAt = nowMs()
		}
		if nowMs()-t0 > (stopAfterS+120)*1000 {
			panic("job still " + str(job["status"]))
		}
	}
	endedAt := ms(job["ended_at"])
	var stopOut any
	if stop != nil {
		stop["seconds_to_ended"] = secs(endedAt - stopAt)
		stopOut = stop
	}
	kept := []M{}
	for i, p := range samples {
		if _, ok := p["visits"]; !ok {
			continue
		}
		if i < len(samples)-1 && num(p["visits"]) == num(samples[i+1]["visits"]) {
			continue
		}
		kept = append(kept, M{"s": p["s"], "visits": p["visits"], "sites": p["sites"], "challenges": p["challenges"]})
	}
	out["job"] = M{
		"status": job["status"], "own_session": job["own_session"], "runs": job["runs"], "progress": job["progress"], "error": job["error"],
		"billed_seconds": job["billed_seconds"], "run_seconds": secs(endedAt - ms(job["started_at"])),
		"stop": stopOut, "progress_samples": kept,
	}

	// 3. The visit history: the job's results log every page it browsed.
	lines := []M{}
	var cursor any = 0.0
	for cursor != nil {
		_, r := call("GET", fmt.Sprintf("/v1/jobs/%s/results?cursor=%v", id, cursor), nil)
		for _, l := range arr(r["results"]) {
			lines = append(lines, obj(l))
		}
		if r["next_cursor"] == cursor {
			break
		}
		cursor = r["next_cursor"]
	}
	visits := []M{}
	cookieLines := 0
	for _, l := range lines {
		switch str(l["type"]) {
		case "visit":
			visits = append(visits, l)
		case "cookies":
			cookieLines++
		}
	}
	host := func(v M) string { u, _ := url.Parse(str(v["url"])); return u.Hostname() }
	firstParty := []string{}
	for _, v := range visits {
		if s := site(host(v)); !contains(firstParty, s) {
			firstParty = append(firstParty, s)
		}
	}
	sort.Strings(firstParty)
	bySiteV := M{}
	for _, s := range firstParty {
		n := 0
		for _, v := range visits {
			if site(host(v)) == s {
				n++
			}
		}
		bySiteV[s] = n
	}
	statusSet := map[int]bool{}
	challenged := 0
	paths := []string{}
	var dwell any
	minD, maxD := math.Inf(1), math.Inf(-1)
	for _, v := range visits {
		statusSet[int(num(v["status"]))] = true
		if v["challenged"] == true {
			challenged++
		}
		u, _ := url.Parse(str(v["url"]))
		paths = append(paths, u.Hostname()+u.EscapedPath())
		minD, maxD = math.Min(minD, num(v["dwell_s"])), math.Max(maxD, num(v["dwell_s"]))
	}
	if len(visits) > 0 {
		dwell = M{"min": minD, "max": maxD}
	}
	statuses := []int{}
	for s := range statusSet {
		statuses = append(statuses, s)
	}
	sort.Ints(statuses)
	out["visits"] = M{"count": len(visits), "challenged": challenged, "by_site": bySiteV, "statuses": statuses, "dwell_s": dwell, "paths": paths, "cookie_lines": cookieLines}

	// 4. The cookies it left: GET /v1/jobs/{id}/cookies (after the end, the last set it delivered).
	ckStatus, ckBody := call("GET", "/v1/jobs/"+id+"/cookies", nil)
	jobCookies := []ck{}
	for _, x := range arr(ckBody["cookies"]) {
		c := obj(x)
		exp := -1.0
		if e, ok := c["expires"]; ok {
			exp = num(e)
		}
		jobCookies = append(jobCookies, ck{str(c["name"]), str(c["domain"]), str(c["path"]), str(c["sameSite"]), exp, c["httpOnly"] == true})
	}
	jc := M{"status": ckStatus, "live": ckBody["live"], "at_vs_end_seconds": secs(ms(ckBody["at"]) - endedAt)}
	for k, v := range summarize(jobCookies, firstParty, nowMs()/1000) {
		jc[k] = v
	}
	out["job_cookies"] = jc

	// 5. The profile, saved after the job's browser exited.
	var prof M
	for i := 0; i < 60; i++ {
		prof = profileInfo()
		if prof != nil && num(prof["version"]) >= 1 && prof["in_use"] == false {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if prof != nil {
		out["profile"] = M{"engine": prof["engine"], "version": prof["version"], "bytes": prof["bytes"], "saved_after_end_seconds": secs(ms(prof["saved_at"]) - endedAt)}
	} else {
		out["profile"] = nil
	}

	// 6. A later session on the profile (read-only: it sees the profile and doesn't change it), then a fresh one.
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		panic(err)
	}
	defer pw.Stop()
	later := inspect(pw, M{"id": name, "mode": "readonly"})
	keys := map[string]bool{}
	for _, c := range later.cookiesAtStart {
		keys[c.key()] = true
	}
	jobKeys := map[string]bool{}
	sessionIn, sessionRestored := 0, 0
	for _, c := range jobCookies {
		jobKeys[c.key()] = true
		if !(c.Expires > 0) {
			sessionIn++
			if keys[c.key()] {
				sessionRestored++
			}
		}
	}
	restored, notInJob := 0, 0
	for k := range jobKeys {
		if keys[k] {
			restored++
		}
	}
	for k := range keys {
		if !jobKeys[k] {
			notInJob++
		}
	}
	out["later"] = M{
		"cookies_at_start": summarize(later.cookiesAtStart, firstParty, nowMs()/1000), "job_cookies_restored": restored,
		"job_cookies_distinct": len(jobKeys), "session_cookies_restored": sessionRestored, "session_cookies_in_job": sessionIn,
		"not_in_job": notInJob, "origins": later.origins, "seconds": later.seconds,
	}
	fresh := inspect(pw, nil)
	out["fresh"] = M{"cookies_at_start": len(fresh.cookiesAtStart), "origins": fresh.origins, "seconds": fresh.seconds}

	// 7. The bill: the warmup's own session and the two later sessions, from the session history.
	since := url.QueryEscape(time.UnixMilli(int64(t0) - 60000).UTC().Format("2006-01-02T15:04:05.000Z"))
	wanted := []string{str(job["session_id"]), later.sessionID, fresh.sessionID}
	sid := func(s M) string {
		if v := str(s["session_id"]); v != "" {
			return v
		}
		return str(s["id"])
	}
	var sessions []any
	for i := 0; i < 15; i++ {
		_, r := call("GET", "/v1/me/sessions?from="+since+"&limit=200", nil)
		sessions = arr(r["sessions"])
		all := true
		for _, w := range wanted {
			found := false
			for _, x := range sessions {
				if sid(obj(x)) == w && str(obj(x)["ended_at"]) != "" {
					found = true
				}
			}
			all = all && found
		}
		if all {
			break
		}
		time.Sleep(2 * time.Second)
	}
	bill := func(id string) any {
		for _, x := range sessions {
			s := obj(x)
			if sid(s) != id {
				continue
			}
			var seconds any
			if str(s["ended_at"]) != "" {
				seconds = secs(ms(s["ended_at"]) - ms(s["started_at"]))
			}
			return M{"engine": s["engine"], "weight": s["weight"], "seconds": seconds}
		}
		return nil
	}
	out["bill"] = M{
		"warmup": M{"billed_seconds": job["billed_seconds"], "session": bill(str(job["session_id"]))},
		"later":  bill(later.sessionID), "fresh": bill(fresh.sessionID),
	}
}
