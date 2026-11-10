// Go 1.22, standard library only. env: CDPFLEET_API_KEY (five countries in one call hold five threads: a paid plan)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const api = "https://cdpfleet.com"
const maxWait = 45

var (
	key       = os.Getenv("CDPFLEET_API_KEY")
	countries = []string{"us", "de", "gb", "jp", "br"} // the first one is the baseline the diff compares with
	formats   = []string{"markdown", "html"}
	pagesList = [][2]string{
		{"Steam store page", "https://store.steampowered.com/app/620/Portal_2/"},
		{"Google home page", "https://www.google.com/"},
		{"Nike home page", "https://www.nike.com/"},
		{"example.com (control)", "https://example.com/"},
	}
	// Prices in the five countries' currencies: R$ before $, the symbol before or after the number.
	// Plain [0-9] and [ \x{00a0}] rather than \d and \s, so the pattern means the same in all five languages.
	price = regexp.MustCompile(`R\$[ \x{00a0}]?[0-9][0-9.,]*|US\$[ \x{00a0}]?[0-9][0-9.,]*|£[ \x{00a0}]?[0-9][0-9.,]*|€[ \x{00a0}]?[0-9][0-9.,]*` +
		`|[0-9][0-9.,]*[ \x{00a0}]?€|[¥￥][ \x{00a0}]?[0-9][0-9,]*|[0-9][0-9,]*[ \x{00a0}]?円|\$[ \x{00a0}]?[0-9][0-9.,]*`)
	// The "accept all" button of a cookie banner, in the five countries' languages.
	consent  = regexp.MustCompile(`(?i)Accept all|Alle akzeptieren|Aceitar todos|Aceitar tudo|すべて同意|すべて受け入れる|すべて許可`)
	trailing = regexp.MustCompile(`[.,]$`)
	langRe   = regexp.MustCompile(`(?i)<html[^>]*\slang="([^"]+)"`)
	titleRe  = regexp.MustCompile(`(?i)<title[^>]*>([^<]*)<`)
	since    = time.Now().UTC().Add(-60 * time.Second).Format(time.RFC3339)
	client   = &http.Client{Timeout: (maxWait + 30) * time.Second}
)

type M = map[string]any

// Keys in insertion order, so every language prints the same JSON layout.
type obj struct {
	keys []string
	vals map[string]any
}

func newObj() *obj { return &obj{vals: map[string]any{}} }
func (o *obj) set(k string, v any) *obj {
	if _, ok := o.vals[k]; !ok {
		o.keys = append(o.keys, k)
	}
	o.vals[k] = v
	return o
}
func (o *obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := marshal(o.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

func str(m M, k string) any {
	if s, ok := m[k].(string); ok {
		return s
	}
	return nil
}
func text(m M, k string) string { s, _ := m[k].(string); return s }
func sub(m M, k string) M       { s, _ := m[k].(M); return s }
func nbytes(v any) int          { s, _ := v.(string); return len(s) }

// A diff's size, or nil when the diff is null (this country or the baseline failed).
func diffBytes(v any) any {
	if v == nil {
		return nil
	}
	return nbytes(v)
}
func attr(re *regexp.Regexp, html string) any {
	if m := re.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return nil
}
func round1(v float64) float64 { return math.Round(v*10) / 10 }

func prices(t string) []string {
	out := []string{}
	for _, p := range price.FindAllString(t, -1) {
		out = append(out, strings.Replace(trailing.ReplaceAllString(p, ""), "\u00a0", " ", 1))
	}
	return out
}
func currency(p string) string {
	switch {
	case strings.HasPrefix(p, "R$"):
		return "BRL"
	case strings.Contains(p, "£"):
		return "GBP"
	case strings.Contains(p, "€"):
		return "EUR"
	case strings.ContainsAny(p, "¥￥円"):
		return "JPY"
	}
	return "USD"
}

// What the Function's unified diff (one country against the baseline) says.
func readDiff(v any) any {
	d, ok := v.(string)
	if !ok {
		return nil
	}
	lines := strings.Split(d, "\n")
	was, now := []string{}, []string{}
	added, removed, hunks := 0, 0, 0
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "@@"):
			hunks++
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			added++
			now = append(now, prices(l)...)
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			removed++
			was = append(was, prices(l)...)
		}
	}
	changes := []string{}
	for i := 0; i < len(was) && i < len(now) && len(changes) < 3; i++ {
		if was[i] != now[i] {
			changes = append(changes, was[i]+" → "+now[i])
		}
	}
	return newObj().set("bytes", len(d)).set("hunks", hunks).set("added", added).set("removed", removed).set("price_changes", changes)
}

func get(path string) M {
	req, _ := http.NewRequest("GET", api+path, nil)
	req.Header.Set("x-api-key", key)
	res, err := client.Do(req)
	if err != nil {
		return M{}
	}
	defer res.Body.Close()
	var m M
	json.NewDecoder(res.Body).Decode(&m)
	return m
}

// The call in the account's history (its x-call-id is the session id there): weight and billed thread-seconds.
func billed(id string) any {
	for i := 0; i < 30 && id != ""; i++ {
		sessions, _ := get("/v1/me/sessions?limit=200&from=" + url.QueryEscape(since))["sessions"].([]any)
		for _, x := range sessions {
			s, _ := x.(M)
			if text(s, "session_id") != id || text(s, "ended_at") == "" {
				continue
			}
			start, _ := time.Parse(time.RFC3339Nano, text(s, "started_at"))
			end, _ := time.Parse(time.RFC3339Nano, text(s, "ended_at"))
			weight, _ := s["weight"].(float64)
			return newObj().set("engine", str(s, "engine")).set("weight", int(weight)).
				set("thread_seconds", math.Round(float64(end.Sub(start).Milliseconds())*weight/100)/10)
		}
		time.Sleep(2 * time.Second)
	}
	return nil
}

type call struct {
	status int
	header http.Header
	raw    []byte
	body   M
	ms     int64
}

func geo(u string, cs []string, wait int, fs []string) call {
	payload, _ := json.Marshal(M{"url": u, "countries": cs, "formats": fs, "max_wait": wait})
	t := time.Now()
	req, _ := http.NewRequest("POST", api+"/v1/functions/geo", bytes.NewReader(payload))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	body := M{}
	json.Unmarshal(raw, &body)
	return call{res.StatusCode, res.Header, raw, body, time.Since(t).Milliseconds()}
}
func loaded(r M) bool {
	s, ok := r["status"].(float64)
	return ok && s >= 200 && s < 400
}
func header(h http.Header, k string) any {
	if v := h.Get(k); v != "" {
		return v
	}
	return nil
}

func main() {
	pages := []*obj{}
	type ids struct{ call, first string }
	lookups := []ids{}
	for _, p := range pagesList { // one call per page, one after another
		first := geo(p[1], countries, maxWait, formats)
		c := first
		// Every diff is against the first country: if that one failed, the call is retried once.
		retried := false
		if c.status == 200 && !loaded(sub(sub(c.body, "results"), countries[0])) {
			c = geo(p[1], countries, maxWait, formats)
			retried = true
		}
		results, diff := sub(c.body, "results"), sub(c.body, "diff")
		baseMd := text(sub(results, countries[0]), "markdown")
		perCountry := newObj()
		for _, cc := range countries {
			r := sub(results, cc)
			html, md := text(r, "html"), text(r, "markdown")
			found := prices(md)
			curr := []string{}
			for _, x := range found {
				cur := currency(x)
				seen := false
				for _, y := range curr {
					seen = seen || y == cur
				}
				if !seen {
					curr = append(curr, cur)
				}
			}
			var title any
			if t := attr(titleRe, html); t != nil {
				title = strings.TrimSpace(t.(string))
			}
			var consentBanner, same, d any
			if html != "" {
				consentBanner = consent.MatchString(html)
			}
			if cc != countries[0] && md != "" && baseMd != "" {
				same = md == baseMd
			}
			// A diff is a string ("" = identical), or null when this country or the baseline failed.
			if cc != countries[0] {
				d = newObj().set("markdown", readDiff(sub(diff, "markdown")[cc])).set("html_bytes", diffBytes(sub(diff, "html")[cc]))
			}
			partial, _ := r["partial"].(bool)
			var status any
			if s, ok := r["status"].(float64); ok {
				status = int(s)
			}
			perCountry.set(cc, newObj().set("status", status).set("error", str(r, "error")).set("partial", partial).
				set("lang", attr(langRe, html)).set("url", str(r, "url")).set("title", title).
				set("currencies", curr).set("prices", found[:min(4, len(found))]).set("consent_banner", consentBanner).
				set("markdown_bytes", len(md)).set("html_bytes", len(html)).set("same_markdown_as_baseline", same).set("diff", d))
		}
		var firstAttempt any
		if retried {
			errors, diffs := newObj(), newObj()
			fresults := sub(first.body, "results")
			for _, cc := range countries {
				if !loaded(sub(fresults, cc)) {
					errors.set(cc, str(sub(fresults, cc), "error"))
				}
			}
			for _, cc := range countries[1:] {
				if v, ok := sub(sub(first.body, "diff"), "markdown")[cc]; ok {
					diffs.set(cc, diffBytes(v))
				}
			}
			firstAttempt = newObj().set("call_id", header(first.header, "x-call-id")).set("errors", errors).set("diffs", diffs)
		}
		diffBytes := newObj()
		for _, f := range []string{"markdown", "html"} {
			n := 0
			for _, v := range sub(diff, f) {
				n += nbytes(v)
			}
			diffBytes.set(f, n)
		}
		ok := []string{}
		for _, cc := range countries {
			if loaded(sub(results, cc)) {
				ok = append(ok, cc)
			}
		}
		attempts := 1
		if retried {
			attempts = 2
		}
		pages = append(pages, newObj().set("page", p[0]).set("url", p[1]).set("attempts", attempts).set("first_attempt", firstAttempt).
			set("status", c.status).set("error", str(c.body, "error")).set("seconds", round1(float64(c.ms)/1000)).
			set("call_id", header(c.header, "x-call-id")).set("warning", header(c.header, "x-cdpfleet-warning")).
			set("response_bytes", len(c.raw)).set("diff_bytes", diffBytes).set("ok_countries", ok).set("countries", perCountry))
		l := ids{call: c.header.Get("x-call-id")}
		if retried {
			l.first = first.header.Get("x-call-id")
		}
		lookups = append(lookups, l)
	}
	// What a failure looks like: the same Steam page with max_wait 1, so no country can load it in time.
	failed := geo("https://store.steampowered.com/app/620/Portal_2/", []string{"us", "de"}, 1, []string{"markdown"})
	failedResults := newObj()
	for _, cc := range []string{"us", "de"} {
		if r, ok := sub(failed.body, "results")[cc].(M); ok {
			var status any
			if s, ok := r["status"].(float64); ok {
				status = int(s)
			}
			failedResults.set(cc, newObj().set("status", status).set("error", str(r, "error")))
		}
	}
	var failedDiff any
	if v, ok := sub(sub(failed.body, "diff"), "markdown")["de"]; ok {
		failedDiff = newObj().set("de", v)
	}
	failure := newObj().set("status", failed.status).set("max_wait", 1).set("results", failedResults).set("diff", failedDiff)

	for i, p := range pages {
		p.set("billed", billed(lookups[i].call))
		if lookups[i].first != "" {
			p.vals["first_attempt"].(*obj).set("billed", billed(lookups[i].first))
		}
	}
	out, err := marshal(newObj().set("language", "go").set("countries", countries).set("formats", formats).set("max_wait", maxWait).
		set("pages", pages).set("failure", failure).set("measured_at", time.Now().UTC().Format("2006-01-02")))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, out, "", "  ")
	fmt.Println(pretty.String())
}
