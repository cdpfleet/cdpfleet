// Go 1.22, standard library only. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
package main

import (
	"archive/zip"
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
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const api = "https://cdpfleet.com"
const site = "https://books.toscrape.com/" // a sandbox made for scraping practice
const stopAt = 30                          // stop the job once this many pages are done, then resume it

var key = os.Getenv("CDPFLEET_API_KEY")
var crawl = map[string]any{"max_depth": 2, "max_pages": 100, "concurrency": 4, "delay_ms": 250, "formats": []string{"markdown"}}
var extract = map[string]string{"title": "h1", "price": ".product_main .price_color"}
var ended = map[string]bool{"done": true, "failed": true, "stopped": true, "interrupted": true}
var client = &http.Client{Timeout: 120 * time.Second}

type M = map[string]any

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// call sends a request with the API key (when withKey) and returns the status, headers and body.
func call(method, url string, body any, withKey bool) (int, http.Header, []byte) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(must(json.Marshal(body)))
	}
	req := must(http.NewRequest(method, url, rd))
	if withKey {
		req.Header.Set("x-api-key", key)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res := must(client.Do(req))
	defer res.Body.Close()
	return res.StatusCode, res.Header, must(io.ReadAll(res.Body))
}

func apiJSON(method, path string, body any) (int, http.Header, M) {
	status, h, b := call(method, api+path, body, true)
	var m M
	_ = json.Unmarshal(b, &m)
	return status, h, m
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func obj(v any) M {
	m, _ := v.(M)
	return m
}

// waitFor polls the job every 3 s until until(job) holds.
func waitFor(id string, until func(M) bool, d time.Duration) M {
	var job M
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(3 * time.Second) {
		_, _, job = apiJSON("GET", "/v1/jobs/"+id, nil)
		if job != nil && until(job) {
			return job
		}
	}
	log.Fatalf("job %s: still %v after %v", id, job["status"], d)
	return nil
}

// stored returns the results stored so far, part by part.
func stored(id string) []M {
	var rows []M
	cursor := any(0.0)
	for cursor != nil {
		_, _, r := apiJSON("GET", fmt.Sprintf("/v1/jobs/%s/results?cursor=%v", id, cursor), nil)
		for _, x := range r["results"].([]any) {
			rows = append(rows, x.(M))
		}
		if r["next_cursor"] == cursor { // nothing new yet
			break
		}
		cursor = r["next_cursor"]
	}
	return rows
}

func hookItems(hook string) []M {
	_, _, b := call("GET", hook, nil, false)
	var r struct{ Items []M }
	must(0, json.Unmarshal(b, &r))
	return r.Items
}

func main() {
	// 1. Map: the site's URLs in one instant call (sitemaps + the start page's links).
	t := time.Now()
	mapStatus, mapHeaders, m := apiJSON("POST", "/v1/functions/map", M{"url": site, "max_urls": 5000})
	urlsFound, categories := 0, 0
	if list, ok := m["urls"].([]any); ok {
		urlsFound = len(list)
		for _, u := range list {
			if strings.Contains(str(u), "/catalogue/category/") {
				categories++
			}
		}
	}
	var partial any
	if p := mapHeaders.Get("x-cdpfleet-partial"); p != "" {
		partial = p
	}
	mapOut := M{"status": mapStatus, "seconds": round1(time.Since(t).Seconds()), "urls": urlsFound, "sources": m["sources"], "partial": partial, "categories": categories}

	// 2. Start the crawl as a job: a zip archive at the end, and webhooks when it's done and when the zip is ready.
	// The receiver here is cdpfleet.com's own test inbox (keeps deliveries an hour, read back with GET);
	// use your own public https endpoint.
	token := make([]byte, 18)
	must(rand.Read(token))
	hook := api + "/e2e/hook/" + base64.RawURLEncoding.EncodeToString(token)
	body := M{"url": site, "extract": extract, "deliver": M{"webhook": M{"url": hook, "events": []string{"done", "archive"}}, "format": "archive"}}
	for k, v := range crawl {
		body[k] = v
	}
	tStart := time.Now()
	startStatus, _, start := apiJSON("POST", "/v1/functions/crawl", body)
	if startStatus != 202 {
		log.Fatalf("crawl %d %v", startStatus, start)
	}
	id := str(start["job_id"])

	// 3. Watch it, and stop it part-way.
	before := waitFor(id, func(j M) bool { return num(obj(j["progress"])["done"]) >= stopAt || ended[str(j["status"])] }, 10*time.Minute)
	_, _, stopReply := apiJSON("DELETE", "/v1/jobs/"+id, nil)
	tStop := time.Now()
	stopped := waitFor(id, func(j M) bool { return ended[str(j["status"])] }, 5*time.Minute)
	tStopped := time.Now()
	atStop := stored(id)
	hooksAtStop := []string{}
	for _, d := range hookItems(hook) {
		var b M
		_ = json.Unmarshal([]byte(str(d["body"])), &b)
		hooksAtStop = append(hooksAtStop, fmt.Sprintf("%s %v", str(d["event"]), obj(b["data"])["status"]))
	}
	_, _, archiveAtStop := apiJSON("GET", "/v1/jobs/"+id+"/archive", nil)

	// 4. Resume: a new run of the same job, from its checkpoint.
	tResume := time.Now()
	resumeStatus, _, resume := apiJSON("POST", "/v1/jobs/"+id+"/resume", nil)
	// 5. Wait for the second run to end.
	done := waitFor(id, func(j M) bool { return num(j["runs"]) >= 2 && ended[str(j["status"])] }, 15*time.Minute)
	tDone := time.Now()

	// 6a. Every result as one JSON-lines file.
	_, _, jsonl := call("GET", api+"/v1/jobs/"+id+"/results.jsonl", nil, true)
	var rows []M
	for _, line := range strings.Split(strings.TrimSpace(string(jsonl)), "\n") {
		var r M
		must(0, json.Unmarshal([]byte(line), &r))
		rows = append(rows, r)
	}
	urls, firstRun := map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		urls[str(r["url"])] = true
	}
	for _, r := range atStop {
		firstRun[str(r["url"])] = true
	}
	secondNew := 0
	for u := range urls {
		if !firstRun[u] {
			secondNew++
		}
	}
	status200, withMarkdown, booksWithPrice, categoryPages := 0, 0, 0, 0
	errors, sample := []M{}, []M{}
	depthCount, statuses := map[string]int{}, map[string]int{}
	for _, r := range rows {
		if num(r["status"]) == 200 {
			status200++
		}
		if r["status"] == nil {
			statuses["null"]++
		} else {
			statuses[fmt.Sprint(r["status"])]++
		}
		if e, ok := r["error"]; ok && e != nil {
			msg := fmt.Sprint(e)
			if len(msg) > 120 {
				msg = msg[:120]
			}
			errors = append(errors, M{"url": r["url"], "status": r["status"], "error": msg})
		}
		if md, ok := r["markdown"].(string); ok && md != "" {
			withMarkdown++
		}
		if price, ok := obj(r["extract"])["price"].(string); ok {
			booksWithPrice++
			if len(sample) < 2 {
				sample = append(sample, M{"url": r["url"], "title": obj(r["extract"])["title"], "price": price})
			}
		}
		if strings.Contains(str(r["url"]), "/catalogue/category/") {
			categoryPages++
		}
		depthCount[fmt.Sprint(r["depth"])]++
	}

	// 6b. The same results as a zip (built when the job ended).
	waitFor(id, func(j M) bool { s := str(obj(j["archive"])["status"]); return s == "ready" || s == "failed" }, 5*time.Minute)
	_, _, archive := apiJSON("GET", "/v1/jobs/"+id+"/archive", nil)
	_, _, zipBytes := call("GET", str(archive["url"]), nil, false) // a storage link, no key
	zr := must(zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes))))
	names := map[string]bool{}
	markdownFiles, extractFiles, indexLines := 0, 0, 0
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.HasSuffix(f.Name, "/index.md") {
			markdownFiles++
		}
		if strings.HasSuffix(f.Name, "/extract.json") {
			extractFiles++
		}
		if f.Name == "index.jsonl" {
			rc := must(f.Open())
			text := strings.TrimSpace(string(must(io.ReadAll(rc))))
			rc.Close()
			if text != "" {
				indexLines = len(strings.Split(text, "\n"))
			}
		}
	}
	has := []string{}
	for _, n := range []string{"index.jsonl", "errors.jsonl", "job.json"} {
		if names[n] {
			has = append(has, n)
		}
	}

	// 7. The webhooks: every delivery is signed with the account's secret, as HMAC-SHA256(secret, "<t>.<raw body>").
	_, _, sec := apiJSON("GET", "/v1/me/webhook-secret", nil)
	secret := str(sec["secret"])
	uuid := regexp.MustCompile(`^[0-9a-f-]{36}$`)
	type delivery struct {
		out      M
		signedAt int64
		data     any
	}
	check := func(d M) delivery {
		parts := map[string]string{}
		for _, kv := range strings.Split(str(d["signature"]), ",") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				parts[k] = v
			}
		}
		raw := str(d["body"])
		var b M
		_ = json.Unmarshal([]byte(raw), &b)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(parts["t"] + "." + raw))
		want := hex.EncodeToString(mac.Sum(nil))
		signedAt, _ := strconv.ParseInt(parts["t"], 10, 64)
		return delivery{
			out: M{
				"event": d["event"], "status": obj(b["data"])["status"], "job_id_matches": str(b["job_id"]) == id,
				"delivery_id_is_uuid": uuid.MatchString(str(d["delivery"])),
				"signature_valid":     parts["v1"] != "" && hmac.Equal([]byte(parts["v1"]), []byte(want)),
			},
			signedAt: signedAt, data: b["data"],
		}
	}
	var deliveries []delivery
	var final, archiveHook *delivery
	for end := time.Now().Add(90 * time.Second); time.Now().Before(end); time.Sleep(3 * time.Second) {
		deliveries = nil
		for _, d := range hookItems(hook) {
			deliveries = append(deliveries, check(d))
		}
		final, archiveHook = nil, nil
		for i := range deliveries {
			if deliveries[i].out["event"] == "job.done" && deliveries[i].out["status"] == "done" {
				final = &deliveries[i]
			}
			if deliveries[i].out["event"] == "job.archive" {
				archiveHook = &deliveries[i]
			}
		}
		if final != nil && archiveHook != nil {
			break
		}
	}
	deliveryOut := []M{}
	for _, d := range deliveries {
		deliveryOut = append(deliveryOut, d.out)
	}
	endedAt := must(time.Parse(time.RFC3339Nano, str(done["ended_at"])))
	var finalOut any
	if final != nil {
		finalOut = M{"data": final.data, "signed_seconds_after_end": final.signedAt - endedAt.Unix()}
	}
	// job.archive carries the zip's size and a download link, like GET /v1/jobs/{id}/archive.
	var archiveHookOut any
	if archiveHook != nil {
		data := obj(archiveHook.data)
		url, hasURL := data["url"].(string)
		var downloaded any
		if hasURL {
			_, _, b := call("GET", url, nil, false)
			downloaded = len(b)
		}
		archiveHookOut = M{
			"status": data["status"], "bytes": data["bytes"], "has_url": hasURL, "url_downloads_bytes": downloaded,
			"signed_seconds_after_end": archiveHook.signedAt - endedAt.Unix(),
		}
	}

	// 8. What it cost: a run holds `concurrency` threads, billed_seconds is the total in thread-seconds.
	billed := num(done["billed_seconds"])
	runSeconds := billed / num(done["concurrency"])
	pages := float64(len(rows))
	depths := M{}
	keys := make([]string, 0, len(depthCount))
	for k := range depthCount {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		depths[k] = depthCount[k]
	}
	results := obj(done["results"])
	stopReplyStatus := stopReply["status"]

	out := M{
		"language": "go",
		"site":     site,
		"map":      mapOut,
		"crawl":    M{"max_depth": 2, "max_pages": 100, "concurrency": 4, "delay_ms": 250, "formats": crawl["formats"], "extract": []string{"title", "price"}, "start_status": startStatus},
		"stop": M{
			"requested_at_done": num(obj(before["progress"])["done"]), "reply": stopReplyStatus,
			"seconds_to_stopped": round1(tStopped.Sub(tStop).Seconds()), "status": stopped["status"], "progress": stopped["progress"],
			"stored_results": len(atStop), "webhooks_by_then": hooksAtStop, "archive": archiveAtStop["status"],
		},
		"resume": M{"status": resumeStatus, "reply": resume["status"]},
		"job": M{
			"status": done["status"], "runs": done["runs"], "progress": done["progress"], "concurrency": done["concurrency"],
			"billed_seconds": done["billed_seconds"], "results": M{"parts": results["parts"], "bytes": results["bytes"]},
			"started_at": done["started_at"], "ended_at": done["ended_at"], "expires_at": done["expires_at"],
		},
		"results_jsonl": M{
			"lines": len(rows), "distinct_urls": len(urls), "bytes": len(jsonl),
			"status_200": status200, "statuses": statuses, "errors": errors, "with_markdown": withMarkdown, "books_with_price": booksWithPrice,
			"category_pages": categoryPages, "depths": depths, "first_run": len(firstRun), "second_run_new": secondNew, "sample": sample,
		},
		"archive": M{
			"status": archive["status"], "bytes": archive["bytes"], "downloaded_bytes": len(zipBytes), "files": len(zr.File),
			"markdown_files": markdownFiles, "extract_files": extractFiles, "index_lines": indexLines, "has": has,
		},
		"webhook": M{"deliveries": deliveryOut, "final_done": finalOut, "archive": archiveHookOut},
		"cost": M{
			"billed_thread_seconds": billed, "pages": len(rows), "run_seconds": round1(runSeconds),
			"pages_per_minute":        round1(pages / (runSeconds / 60)),
			"thread_seconds_per_page": round1(billed / pages),
			"client_running_seconds":  round1(tStopped.Sub(tStart).Seconds() + tDone.Sub(tResume).Seconds()),
		},
		"measured_at": time.Now().UTC().Format("2006-01-02"),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	must(0, enc.Encode(out))
}
