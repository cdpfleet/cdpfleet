// Go 1.22, standard library only. env: CDPFLEET_API_KEY (a paid plan: monitors aren't on Community)
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const api = "https://cdpfleet.com"
const page = "https://quotes.toscrape.com/random" // a practice site: a different quote on every load
const maxRuns = 5                                 // checks to trigger at most while waiting for a change (the same quote can come up twice)

var key = os.Getenv("CDPFLEET_API_KEY")
var client = &http.Client{Timeout: 120 * time.Second}
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type M = map[string]any

func must[T any](v T, err error) T {
	if err != nil {
		panic(err) // not log.Fatal: the deferred DELETE must still run
	}
	return v
}

// call sends a request with the API key (when withKey) and returns the status and body.
func call(method, u string, body any, withKey bool) (int, []byte) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(must(json.Marshal(body)))
	}
	req := must(http.NewRequest(method, u, rd))
	if withKey {
		req.Header.Set("x-api-key", key)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res := must(client.Do(req))
	defer res.Body.Close()
	return res.StatusCode, must(io.ReadAll(res.Body))
}

func apiJSON(method, path string, body any) (int, M) {
	status, b := call(method, api+path, body, true)
	var m M
	_ = json.Unmarshal(b, &m)
	return status, m
}

func num(v any) float64       { f, _ := v.(float64); return f }
func str(v any) string        { s, _ := v.(string); return s }
func obj(v any) M             { m, _ := v.(M); return m }
func arr(v any) []any         { a, _ := v.([]any); return a }
func secs(ms float64) float64 { return math.Round(ms/100) / 10 }
func nowMs() float64          { return float64(time.Now().UnixMilli()) }

// ms: an API timestamp as epoch milliseconds.
func ms(iso any) float64 {
	t, err := time.Parse(time.RFC3339Nano, str(iso))
	if err != nil {
		return 0
	}
	return float64(t.UnixMilli())
}

// verify: x-cdpfleet-signature is t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>. Check it against the
// raw bytes received, before parsing them, and refuse old timestamps (replays).
func verify(secret, header, rawBody string) bool {
	parts := map[string]string{}
	for _, kv := range strings.Split(header, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			parts[k] = v
		}
	}
	t, err := strconv.ParseInt(parts["t"], 10, 64)
	given, err2 := hex.DecodeString(parts["v1"])
	if err != nil || err2 != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts["t"] + "." + rawBody))
	return hmac.Equal(given, mac.Sum(nil)) && math.Abs(float64(time.Now().Unix()-t)) < 300
}

func get(u string) string {
	_, b := call("GET", u, nil, false)
	return string(b)
}

func main() {
	since := time.Now().Add(-60 * time.Second).UTC().Format(time.RFC3339)
	// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour, raw body and
	// headers, and reads them back with GET. Use your own public https endpoint.
	tok := make([]byte, 18)
	must(rand.Read(tok))
	hook := api + "/e2e/hook/" + base64.RawURLEncoding.EncodeToString(tok)
	inbox := func() []any {
		var r M
		_ = json.Unmarshal([]byte(get(hook)), &r)
		return arr(r["items"])
	}
	_, sec := apiJSON("GET", "/v1/me/webhook-secret", nil)
	secret := str(sec["secret"])

	// 1. Create the monitor: only the quote element, as text; the tags dropped (remove) and the "(about)" link
	// label taken out of the text (ignore) before comparing.
	// every: "1h" is its own schedule; the program doesn't wait for it and triggers checks with POST …/run.
	spec := M{"url": page, "format": "text", "selector": ".quote", "remove": []string{".tags"}, "ignore": []string{`\(about\)`}, "every": "1h",
		"deliver": M{"webhook": M{"url": hook}}}
	status, c := apiJSON("POST", "/v1/monitors", spec)
	if status != 201 {
		log.Fatalf("create %d %v", status, c)
	}
	id := str(c["monitor_id"])
	out := M{"language": "go", "page": page}
	defer func() {
		// 7. Delete it, whatever happened above.
		delStatus, del := apiJSON("DELETE", "/v1/monitors/"+id, nil)
		after, _ := apiJSON("GET", "/v1/monitors/"+id, nil)
		out["deleted"] = M{"status": delStatus, "deleted": del["deleted"] == true, "get_after": after}
		if r := recover(); r != nil {
			log.Fatal(r)
		}
		out["measured_at"] = time.Now().UTC().Format("2006-01-02")
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	}()

	_, list := apiJSON("GET", "/v1/monitors", nil)
	listed := false
	for _, m := range arr(list["monitors"]) {
		listed = listed || str(obj(m)["monitor_id"]) == id
	}
	out["monitor"] = M{
		"create_status": status, "every": c["every"], "format": c["format"], "selector": c["selector"], "remove": c["remove"], "ignore": c["ignore"],
		"webhook": str(obj(obj(c["deliver"])["webhook"])["url"]) == hook, "active": c["active"], "listed": listed,
		"first_check_due_seconds": secs(ms(c["next_run_at"]) - ms(c["created_at"])),
	}

	// 2. Check now, and again until the page has changed. Poll the monitor and the inbox every 0.5 s.
	type check struct {
		out     M
		tRun    float64
		lastRun float64
	}
	var checks []check
	var changeN int
	var changeRun, tHook float64
	var item M
	for n := 1; n <= maxRuns; n++ {
		tRun := nowMs()
		runStatus, _ := apiJSON("POST", "/v1/monitors/"+id+"/run", nil)
		var m M
		var tStatus float64
		for end := time.Now().Add(120 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
			if tStatus == 0 {
				_, m = apiJSON("GET", "/v1/monitors/"+id, nil)
				if num(m["checks"]) >= float64(n) {
					tStatus = nowMs()
				}
			}
			if tStatus != 0 && m["last_status"] != "changed" {
				break
			}
			if item == nil {
				for _, d := range inbox() {
					if str(obj(d)["event"]) == "monitor.change" {
						item, tHook = obj(d), nowMs()
						break
					}
				}
			}
			if tStatus != 0 && item != nil {
				break
			}
		}
		if tStatus == 0 {
			panic(fmt.Sprintf("check %d: no result after 120 s", n))
		}
		checks = append(checks, check{M{
			"n": n, "run_status": runStatus, "status": m["last_status"], "error": m["last_error"],
			"seconds_to_status": secs(tStatus - tRun), "check_seconds": secs(ms(m["last_run_at"]) - tRun),
		}, tRun, ms(m["last_run_at"])})
		if m["last_status"] == "changed" {
			changeN, changeRun = n, tRun
			break
		}
	}

	// 3. The webhook: verify it, then compare it with the change as the API keeps it.
	deliveries := inbox()
	changeID := ""
	if item != nil {
		raw := str(item["body"])
		var body M
		_ = json.Unmarshal([]byte(raw), &body)
		data := obj(body["data"])
		changeID = str(data["change_id"])
		t, _ := strconv.ParseInt(regexp.MustCompile(`t=(\d+)`).FindStringSubmatch(str(item["signature"]))[1], 10, 64)
		_, full := apiJSON("GET", "/v1/monitors/"+id+"/changes/"+changeID, nil)
		var before any
		if u := str(full["before_url"]); u != "" {
			before = len(strings.Split(get(u), "\n"))
		}
		after := get(str(full["after_url"]))
		snapshot := get(str(data["snapshot_url"]))
		diff := str(data["diff"])
		lines := strings.Split(diff, "\n")
		removed, added := []string{}, []string{}
		for _, l := range lines {
			if strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---") {
				removed = append(removed, l[1:])
			}
			if strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++") {
				added = append(added, l[1:])
			}
		}
		keys := []string{}
		for k := range data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out["webhook"] = M{
			"deliveries": len(deliveries), "event_header": item["event"], "type": body["type"],
			"delivery_id_is_uuid":    uuidRe.MatchString(str(item["delivery"])),
			"signature_valid":        verify(secret, str(item["signature"]), raw),
			"tampered_body_rejected": !verify(secret, str(item["signature"]), strings.Replace(raw, `"added":`, `"added": `, 1)),
			"monitor_id_matches":     data["monitor_id"] == id, "url": data["url"], "keys": keys,
			"seconds_run_to_seen":        secs(tHook - changeRun),
			"seconds_run_to_signed":      t - int64(changeRun)/1000,
			"signed_within_1s_of_change": math.Abs(float64(t*1000)-ms(data["at"])) < 1000,
		}
		out["change"] = M{
			"after_runs": changeN, "change_id": changeID, "added": data["added"], "removed": data["removed"],
			"diff_bytes": len(diff), "diff_lines": len(lines), "removed_lines": removed, "added_lines": added,
			"diff_has_ignored_text": regexp.MustCompile(`Tags:|\(about\)`).MatchString(diff),
			"api_diff_equal":        str(full["diff"]) == diff,
			"api_counts_equal":      num(full["added"]) == num(data["added"]) && num(full["removed"]) == num(data["removed"]),
			"snapshot_equals_after": snapshot == after, "before_lines": before, "after_lines": len(strings.Split(after, "\n")),
			"after_text": after,
		}
	}

	// 4. The history: every change, newest first, with a preview of its diff.
	_, history := apiJSON("GET", "/v1/monitors/"+id+"/changes", nil)
	ids := []string{}
	for _, ch := range arr(history["changes"]) {
		ids = append(ids, str(obj(ch)["change_id"]))
	}
	_, m := apiJSON("GET", "/v1/monitors/"+id, nil)
	out["history"] = M{
		"changes": len(ids), "change_ids": ids, "newest_is_webhook_change": len(ids) > 0 && ids[0] == changeID,
		"monitor_checks": m["checks"], "monitor_changes": m["changes"], "last_status": m["last_status"],
		"next_run_minutes_after_last_check": math.Round((ms(m["next_run_at"]) - ms(m["last_run_at"])) / 60000),
	}

	// 5. Pause it (PATCH active: false): no more scheduled checks, history kept.
	pStatus, paused := apiJSON("PATCH", "/v1/monitors/"+id, M{"active": false})
	out["pause"] = M{"status": pStatus, "active": paused["active"], "next_run_at": paused["next_run_at"]}

	// 6. What the checks cost. Each check is a scrape the service runs as you, billed like any call: it has no
	// x-call-id you can see, but it is in the account's session history as engine "function:scrape".
	time.Sleep(3 * time.Second)
	_, sess := apiJSON("GET", "/v1/me/sessions?limit=200&from="+url.QueryEscape(since), nil)
	billing := []M{}
	outChecks := []M{}
	for _, ch := range checks {
		var inside []M
		for _, s := range arr(sess["sessions"]) {
			so := obj(s)
			st := ms(so["started_at"])
			if so["engine"] == "function:scrape" && st >= ch.tRun-2000 && st <= ch.lastRun {
				inside = append(inside, so)
			}
		}
		sort.Slice(inside, func(a, b int) bool { return ms(inside[a]["started_at"]) < ms(inside[b]["started_at"]) })
		b := M{"check": ch.out["n"], "sessions_in_window": len(inside), "engine": nil, "weight": nil, "seconds": nil, "thread_seconds": nil}
		if len(inside) > 0 { // the first scrape to start after the run request
			s := inside[0]
			b["engine"], b["weight"] = s["engine"], s["weight"]
			if s["ended_at"] != nil {
				dur := ms(s["ended_at"]) - ms(s["started_at"])
				b["seconds"], b["thread_seconds"] = secs(dur), secs(dur*num(s["weight"]))
			}
		}
		billing = append(billing, b)
		outChecks = append(outChecks, ch.out)
	}
	out["billing"] = billing
	out["checks"] = outChecks
}
