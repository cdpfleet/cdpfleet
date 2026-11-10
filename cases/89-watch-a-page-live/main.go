// Go 1.22, standard library only. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	"sync"
	"time"
)

const api = "https://cdpfleet.com"

// A public page that changes on its own: it asks its server for the ISS position about once a second
// (XHR to /w/ajax/realtime) and rewrites a small table with it.
const page = "https://wheretheiss.at/"
const stopAfterS = 40 // DELETE the job this long after starting it

var key = os.Getenv("CDPFLEET_API_KEY")
var ended = map[string]bool{"done": true, "failed": true, "stopped": true, "interrupted": true}
var types = []string{"xhr", "fetch", "ws", "dom"}
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type M = map[string]any

func send(method, u string, body any, withKey bool) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u, rd)
	if withKey {
		req.Header.Set("x-api-key", key)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func call(method, path string, body any) (int, M) {
	status, b := send(method, api+path, body, true)
	var m M
	json.Unmarshal(b, &m)
	return status, m
}

func str(v any) string { s, _ := v.(string); return s }
func num(v any) float64 { f, _ := v.(float64); return f }
func obj(v any) M { m, _ := v.(M); return m }
func arr(v any) []any { a, _ := v.([]any); return a }
func nowMs() float64 { return float64(time.Now().UnixMilli()) }
func secs(ms float64) float64 { return math.Round(ms/100) / 10 }
func round(x float64, d int) float64 { p := math.Pow(10, float64(d)); return math.Round(x*p) / p }
func ms(iso any) float64 { // an API timestamp
	t, _ := time.Parse(time.RFC3339Nano, str(iso))
	return float64(t.UnixMilli())
}
func evKey(e M) string {
	u := str(e["url"])
	if u == "" {
		u = str(e["selector"])
	}
	return str(e["type"]) + " " + str(e["at"]) + " " + u
}
func byType(events []M) M {
	o := M{}
	for _, t := range types {
		n := 0
		for _, e := range events {
			if str(e["type"]) == t {
				n++
			}
		}
		if n > 0 {
			o[t] = n
		}
	}
	return o
}

// x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>, checked on the raw bytes.
func verify(secret, header, rawBody string) bool {
	parts := map[string]string{}
	for _, kv := range strings.Split(header, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			parts[k] = v
		}
	}
	given, err := hex.DecodeString(parts["v1"])
	if parts["t"] == "" || err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts["t"] + "." + rawBody))
	return hmac.Equal(mac.Sum(nil), given)
}

// Great-circle distance in km.
func km(lat1, lon1, lat2, lon2 float64) float64 {
	r := func(d float64) float64 { return d * math.Pi / 180 }
	h := math.Pow(math.Sin(r(lat2-lat1)/2), 2) + math.Cos(r(lat1))*math.Cos(r(lat2))*math.Pow(math.Sin(r(lon2-lon1)/2), 2)
	return 2 * 6371 * math.Asin(math.Sqrt(h))
}

type live struct {
	ev       M
	received float64
}

func main() {
	// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour (raw body and
	// headers) and reads them back with GET. Use your own public https endpoint.
	tok := make([]byte, 18)
	rand.Read(tok)
	hook := api + "/e2e/hook/" + base64.RawURLEncoding.EncodeToString(tok)
	_, sec := call("GET", "/v1/me/webhook-secret", nil)
	secret := str(sec["secret"])

	// 1. Start the watch: XHR/fetch calls matching the glob, and changes under the selector, with a webhook.
	watch := M{
		"url": page, "browser": "chrome",
		"duration":     300, // a safety net: the program stops the job itself
		"reload_every": 15,
		"filter":       M{"url": []string{"*/w/ajax/realtime*"}, "selector": ".span3 table.table-striped", "types": []string{"xhr", "fetch", "dom"}},
	}
	body := M{"deliver": M{"webhook": M{"url": hook, "events": []string{"event", "done"}}}}
	for k, v := range watch {
		body[k] = v
	}
	t0 := nowMs()
	startStatus, start := call("POST", "/v1/functions/watch", body)
	if startStatus != 202 {
		panic(fmt.Sprintf("watch %d %v", startStatus, start))
	}
	id := str(start["job_id"])

	// 2. Follow it live on the SSE stream; stop it with DELETE after stopAfterS seconds.
	var mu sync.Mutex
	var stop M
	var stopAt float64
	timer := time.AfterFunc(stopAfterS*time.Second, func() {
		t := nowMs()
		status, r := call("DELETE", "/v1/jobs/"+id, nil)
		mu.Lock()
		stop, stopAt = M{"status": status, "reply": r["status"]}, t
		mu.Unlock()
	})

	var events []live
	seen := M{"progress": 0, "part": 0, "event": 0, "done": 0}
	var doneData M
	req, _ := http.NewRequest("GET", api+"/v1/jobs/"+id+"/events", nil)
	req.Header.Set("x-api-key", key)
	req.Header.Set("accept", "text/event-stream")
	sse, err := (&http.Client{Timeout: 240 * time.Second}).Do(req)
	if err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(sse.Body)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	name, data := "", []string{}
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			if strings.HasPrefix(line, "event:") {
				name = strings.TrimSpace(line[6:])
			} else if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(line[5:]))
			}
			continue
		}
		if name != "" { // a blank line ends a message; a keep-alive comment has no event name
			n, _ := seen[name].(int)
			seen[name] = n + 1
			var payload M
			json.Unmarshal([]byte(strings.Join(data, "\n")), &payload)
			if name == "event" {
				for _, ev := range arr(payload["events"]) {
					events = append(events, live{obj(ev), nowMs()})
				}
			}
			if name == "done" {
				doneData = payload
				break
			}
		}
		name, data = "", nil
	}
	sse.Body.Close()
	timer.Stop()
	tEnd := nowMs()

	// 3. The job as it ended, and its events as stored.
	_, job := call("GET", "/v1/jobs/"+id, nil)
	for i := 0; i < 20 && !ended[str(job["status"])]; i++ {
		time.Sleep(2 * time.Second)
		_, job = call("GET", "/v1/jobs/"+id, nil)
	}
	var stored []M
	for cursor := 0.0; ; {
		_, r := call("GET", fmt.Sprintf("/v1/jobs/%s/results?cursor=%d", id, int64(cursor)), nil)
		for _, x := range arr(r["results"]) {
			stored = append(stored, obj(x))
		}
		if r["next_cursor"] == nil || num(r["next_cursor"]) == cursor {
			break
		}
		cursor = num(r["next_cursor"])
	}

	// 4. The webhook deliveries: job.event batches (at most one per 2 s) and job.done. Wait for job.done.
	var items []M
	for end := time.Now().Add(90 * time.Second); time.Now().Before(end); time.Sleep(2 * time.Second) {
		_, b := send("GET", hook, nil, false)
		var box struct{ Items []M }
		json.Unmarshal(b, &box)
		items = box.Items
		found := false
		for _, d := range items {
			found = found || str(d["event"]) == "job.done"
		}
		if found {
			break
		}
	}
	var hookEvents []M
	var sent, delays []float64
	var doneHook M
	byEvent := M{}
	valid, maxPerBatch := 0, 0
	ids := map[string]bool{}
	allUUID, jobMatches := true, true
	sigRe := regexp.MustCompile(`t=(\d+)`)
	for _, d := range items {
		var b M
		json.Unmarshal([]byte(str(d["body"])), &b)
		ev := str(d["event"])
		n, _ := byEvent[ev].(int)
		byEvent[ev] = n + 1
		if verify(secret, str(d["signature"]), str(d["body"])) {
			valid++
		}
		ids[str(d["delivery"])] = true
		allUUID = allUUID && uuidRe.MatchString(str(d["delivery"]))
		jobMatches = jobMatches && str(b["job_id"]) == id
		if ev == "job.done" {
			doneHook = obj(b["data"])
		}
		if ev != "job.event" {
			continue
		}
		evs := arr(obj(b["data"])["events"])
		newest := 0.0
		for _, e := range evs {
			hookEvents = append(hookEvents, obj(e))
			newest = math.Max(newest, ms(obj(e)["at"]))
		}
		maxPerBatch = max(maxPerBatch, len(evs))
		sent = append(sent, ms(b["created_at"]))
		// How long after its newest event each batch was signed (the signature's t, whole seconds).
		if m := sigRe.FindStringSubmatch(str(d["signature"])); m != nil {
			t, _ := strconv.ParseFloat(m[1], 64)
			delays = append(delays, t-newest/1000)
		}
	}
	sort.Float64s(sent)
	sort.Float64s(delays)
	var minGap any
	for i := 1; i < len(sent); i++ {
		if g := secs(sent[i] - sent[i-1]); minGap == nil || g < minGap.(float64) {
			minGap = g
		}
	}
	storedKeys, hookKeys := map[string]bool{}, map[string]bool{}
	for _, e := range stored {
		storedKeys[evKey(e)] = true
	}
	for _, e := range hookEvents {
		hookKeys[evKey(e)] = true
	}

	// 5. What the events say: the ISS position from each XHR response, the table text from each DOM change.
	type pos struct {
		at, lat, lon float64
		time         string
	}
	var positions []pos
	var dom []M
	globOK, noBody := true, 0
	for _, e := range stored {
		switch str(e["type"]) {
		case "xhr":
			globOK = globOK && strings.Contains(str(e["url"]), "/w/ajax/realtime") && num(e["status"]) == 200 && str(e["method"]) == "GET"
			if str(e["response_body"]) == "" { // an XHR still in flight when the page reloads has no body
				noBody++
				continue
			}
			var r M
			json.Unmarshal([]byte(str(e["response_body"])), &r)
			d := obj(r["data"])
			positions = append(positions, pos{ms(e["at"]), num(d["lat"]), num(d["lon"]), str(d["time"])})
		case "dom":
			dom = append(dom, e)
		}
	}
	var lags []float64
	liveEvents := make([]M, len(events))
	sameAsLive := len(stored) == len(events)
	for i, x := range events {
		lags = append(lags, (x.received-ms(x.ev["at"]))/1000)
		liveEvents[i] = x.ev
		sameAsLive = sameAsLive && storedKeys[evKey(x.ev)]
	}
	sort.Float64s(lags)
	firstOf := func(t string) any {
		for _, x := range events {
			if str(x.ev["type"]) == t {
				return secs(x.received - t0)
			}
		}
		return nil
	}

	// 6. The bill: a watch runs in your own browser session, one thread for a headless Chrome.
	since := time.UnixMilli(int64(t0) - 60000).UTC().Format("2006-01-02T15:04:05.000Z")
	_, ss := call("GET", "/v1/me/sessions?from="+url.QueryEscape(since)+"&limit=200", nil)
	var session any
	for _, s := range arr(ss["sessions"]) {
		so := obj(s)
		if sid := str(job["session_id"]); sid != "" && (str(so["session_id"]) == sid || str(so["id"]) == sid) {
			session = M{"engine": so["engine"], "weight": so["weight"], "seconds": secs(ms(so["ended_at"]) - ms(so["started_at"]))}
		}
	}

	watchOut := M{"stop_after_s": stopAfterS, "start_status": startStatus}
	for k, v := range watch {
		watchOut[k] = v
	}
	sseOut := M{
		"events": len(events), "by_type": byType(liveEvents), "messages": seen,
		"first_event_seconds": nil, "first_xhr_seconds": firstOf("xhr"), "first_dom_seconds": firstOf("dom"),
		"lag_seconds": nil, "done_status": doneData["status"], "stream_seconds": secs(tEnd - t0),
	}
	if len(events) > 0 {
		sseOut["first_event_seconds"] = secs(events[0].received - t0)
		sseOut["lag_seconds"] = M{"min": round(lags[0], 2), "median": round(lags[len(lags)/2], 2), "max": round(lags[len(lags)-1], 2)}
	}
	var stopOut any
	mu.Lock()
	if stop != nil {
		stopOut = M{"after_seconds": secs(stopAt - t0), "status": stop["status"], "reply": stop["reply"], "seconds_to_ended": secs(ms(job["ended_at"]) - stopAt)}
	}
	mu.Unlock()
	var results any
	if r := obj(job["results"]); r != nil {
		results = M{"parts": r["parts"], "bytes": r["bytes"]}
	}
	distinct := map[string]bool{}
	for _, p := range positions {
		distinct[fmt.Sprint(p.lat, ",", p.lon)] = true
	}
	storedOut := M{
		"events": len(stored), "by_type": byType(stored), "same_as_live": sameAsLive, "xhr_urls_match_glob": globOK,
		"xhr_without_body": noBody, "positions": len(positions), "distinct_positions": len(distinct),
		"first": nil, "last": nil, "ground_km": nil, "ground_km_per_s": nil,
		"dom_events": len(dom), "dom_text_sample": nil, "dom_html_bytes": nil,
	}
	if len(positions) > 0 {
		f, l := positions[0], positions[len(positions)-1]
		dist := km(f.lat, f.lon, l.lat, l.lon)
		storedOut["first"] = M{"lat": round(f.lat, 3), "lon": round(f.lon, 3), "time": f.time}
		storedOut["last"] = M{"lat": round(l.lat, 3), "lon": round(l.lon, 3), "time": l.time}
		storedOut["ground_km"] = round(dist, 1)
		if l.at > f.at {
			storedOut["ground_km_per_s"] = round(dist/((l.at-f.at)/1000), 2)
		}
	}
	if len(dom) > 0 {
		storedOut["dom_text_sample"] = strings.Join(strings.Fields(str(dom[0]["text"])), " ")
		storedOut["dom_html_bytes"] = len(str(dom[0]["html"]))
	}
	newestSent := 0.0
	for _, e := range hookEvents {
		newestSent = math.Max(newestSent, ms(e["at"]))
	}
	notInHooks, notSentLast := 0, true
	for _, e := range stored {
		if !hookKeys[evKey(e)] {
			notInHooks++
			notSentLast = notSentLast && ms(e["at"]) >= newestSent
		}
	}
	allStored := true
	for _, e := range hookEvents {
		allStored = allStored && storedKeys[evKey(e)]
	}
	var delayOut, doneOut any
	if len(delays) > 0 {
		delayOut = M{"min": round(delays[0], 1), "max": round(delays[len(delays)-1], 1)}
	}
	if doneHook != nil {
		doneOut = M{"status": doneHook["status"], "progress": doneHook["progress"], "error": doneHook["error"]}
	}
	tampered := len(items) > 0 && !verify(secret, str(items[0]["signature"]), str(items[0]["body"])+" ")

	out := M{
		"language": "go",
		"page":     page,
		"watch":    watchOut,
		"sse":      sseOut,
		"stop":     stopOut,
		"job": M{
			"status": job["status"], "own_session": job["own_session"], "concurrency": job["concurrency"], "runs": job["runs"],
			"progress": job["progress"], "billed_seconds": job["billed_seconds"],
			"run_seconds": secs(ms(job["ended_at"]) - ms(job["started_at"])), "results": results, "error": job["error"],
		},
		"session": session,
		"stored":  storedOut,
		"webhook": M{
			"deliveries": len(items), "inbox_full": len(items) >= 20, "by_event": byEvent,
			"signatures_valid": valid, "tampered_body_rejected": tampered,
			"delivery_ids_unique_uuids": len(ids) == len(items) && allUUID, "job_id_matches": jobMatches,
			"events": len(hookEvents), "events_by_type": byType(hookEvents), "events_all_stored": allStored,
			"stored_not_in_webhooks": notInHooks, "not_sent_are_the_last": notSentLast, "signed_after_newest_event_seconds": delayOut,
			"min_gap_seconds": minGap, "max_events_per_batch": maxPerBatch, "done": doneOut,
		},
		"measured_at": time.Now().UTC().Format("2006-01-02"),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
