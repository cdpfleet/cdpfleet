// go get github.com/gorilla/websocket@v1.5.3
// MCP without an SDK (the official Go SDK needs Go 1.23+): JSON-RPC 2.0 over Streamable HTTP.
// env: CDPFLEET_API_KEY (a paid plan: kept downloads aren't on Community), PROXY_URL
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const api = "https://cdpfleet.com"

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL")
var httpc = &http.Client{Timeout: 120 * time.Second}

// Seeded test files: the same URL always returns the same bytes, so a copy can be checked
// against the original fetched directly from here.
var personFiles = []string{"https://httpbingo.org/bytes/204800?seed=82", "https://httpbingo.org/bytes/65536?seed=7"}

const agentFile = "https://httpbingo.org/bytes/102400?seed=79"

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// request sends one HTTP request; the API key goes only to cdpfleet.
func request(method, u string, body any, withKey bool) (int, http.Header, []byte) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(must(json.Marshal(body)))
	}
	req := must(http.NewRequest(method, u, r))
	if withKey {
		req.Header.Set("x-api-key", key)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	res := must(httpc.Do(req))
	defer res.Body.Close()
	return res.StatusCode, res.Header, must(io.ReadAll(res.Body))
}

type File struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Bytes     int    `json:"bytes"`
	Sha256    string `json:"sha256"`
	SourceURL string `json:"source_url"`
}

type Checked struct {
	Name           string  `json:"name"`
	Bytes          int     `json:"bytes"`
	Sha256         string  `json:"sha256"`
	SourceURL      string  `json:"source_url"`
	Link           string  `json:"link"`
	FetchedStatus  int     `json:"fetched_status"`
	FetchedType    string  `json:"fetched_type"`
	FetchedBytes   int     `json:"fetched_bytes"`
	FetchedSha256  string  `json:"fetched_sha256"`
	OriginalBytes  *int    `json:"original_bytes"`
	OriginalSha256 *string `json:"original_sha256"`
}

func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// listed polls the account API until the session's kept files are listed; ms from `since`.
func listed(sessionID string, count int, since time.Time) ([]File, int64) {
	for {
		_, _, data := request("GET", api+"/v1/me/downloads?session_id="+sessionID, nil, true)
		var body struct {
			Files []File `json:"files"`
		}
		json.Unmarshal(data, &body)
		if len(body.Files) >= count || time.Since(since) > 2*time.Minute {
			return body.Files, time.Since(since).Milliseconds()
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// fetchKept fetches each kept file through its private link and compares it with what the list
// said and, for seeded files, with the original.
func fetchKept(files []File, links map[string]string) []Checked {
	sort.Slice(files, func(i, j int) bool { return files[i].Bytes > files[j].Bytes })
	out := []Checked{}
	for _, f := range files {
		link, how := links[f.Name], "from the downloads tool"
		if link == "" {
			_, _, data := request("GET", api+"/v1/me/downloads/"+f.ID+"/url", nil, true)
			var l struct {
				URL       string `json:"url"`
				ExpiresIn int    `json:"expires_in_s"`
			}
			json.Unmarshal(data, &l)
			link, how = l.URL, fmt.Sprintf("GET /v1/me/downloads/{id}/url (%d s)", l.ExpiresIn)
		}
		status, header, got := request("GET", link, nil, false)
		c := Checked{Name: f.Name, Bytes: f.Bytes, Sha256: f.Sha256, SourceURL: f.SourceURL, Link: how,
			FetchedStatus: status, FetchedType: header.Get("content-type"), FetchedBytes: len(got), FetchedSha256: hash(got)}
		if regexp.MustCompile(`[?&]seed=`).MatchString(f.SourceURL) {
			_, _, orig := request("GET", f.SourceURL, nil, false)
			nb, sum := len(orig), hash(orig)
			c.OriginalBytes, c.OriginalSha256 = &nb, &sum
		}
		out = append(out, c)
	}
	return out
}

// ---- 1. A person in Launch interactive: no client, the server opens the start page and
//         a person drives it through a control live link (a scripted stand-in here).
func person() map[string]any {
	status, _, data := request("POST", "https://starter.cdpfleet.com/chromium/session", map[string]any{
		"proxy": proxy, "headless": true, "live_view": true, "record": false, "downloads": true,
		"start_url": "https://httpbingo.org/", "inactivity_timeout": 60}, true)
	if status != 200 {
		log.Fatalf("launch %d %s", status, data)
	}
	var session map[string]any
	json.Unmarshal(data, &session)
	sessionID := session["sessionId"].(string)
	host := must(url.Parse(session["wsUrl"].(string))).Host
	_, _, data = request("POST", "https://"+host+"/admin/session/live", map[string]any{"session_id": sessionID, "mode": "control"}, true)
	var live struct {
		LiveURL  string `json:"live_url"`
		Protocol string `json:"protocol"`
	}
	json.Unmarshal(data, &live)

	// The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
	viewer := must2(websocket.DefaultDialer.Dial(live.LiveURL, nil))
	var mu sync.Mutex
	seen := []string{}
	frames := 0
	go func() {
		for {
			kind, msg, err := viewer.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			if kind == websocket.BinaryMessage {
				frames++
			} else {
				var m struct {
					Type    string `json:"type"`
					Current string `json:"current"`
					Pages   []struct {
						TargetID string `json:"targetId"`
						URL      string `json:"url"`
					} `json:"pages"`
				}
				if json.Unmarshal(msg, &m) == nil && m.Type == "pages" {
					for _, p := range m.Pages {
						if p.TargetID == m.Current && p.URL != "" && (len(seen) == 0 || seen[len(seen)-1] != p.URL) {
							seen = append(seen, p.URL)
						}
					}
				}
			}
			mu.Unlock()
		}
	}()
	shown := func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range seen {
			if s == "https://httpbingo.org/" {
				return true
			}
		}
		return false
	}
	for i := 0; i < 60 && !shown(); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	startShown := shown()

	// The person pastes two file links into the address bar, a few seconds apart.
	for _, u := range personFiles {
		if err := viewer.WriteJSON(map[string]any{"type": "navigate", "url": u}); err != nil {
			log.Fatal(err)
		}
		time.Sleep(3 * time.Second)
	}
	mu.Lock()
	pageAfter, frameCount := seen[len(seen)-1], frames
	mu.Unlock()
	viewer.Close()

	// Closing the dashboard's viewer ends a Launch interactive session: DELETE /v1/me/threads/{id}.
	t := time.Now()
	endStatus, _, _ := request("DELETE", api+"/v1/me/threads/"+sessionID, nil, true)
	ended := time.Now()
	files, ms := listed(sessionID, len(personFiles), ended)
	return map[string]any{
		"launch_downloads": session["downloads"], "resource_class": session["resource_class"], "live_protocol": live.Protocol,
		"start_page_shown": startShown, "frames": frameCount, "pasted": personFiles, "page_after": pageAfter,
		"end_status": endStatus, "end_ms": ended.Sub(t).Milliseconds(), "listed_after_ms": ms, "files": fetchKept(files, nil),
	}
}

func must2[T any, U any](v T, _ U, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// ---- 2. An AI agent through the hosted MCP server, authenticated with the API key.

// Conn is one MCP session.
type Conn struct {
	session string
	nextID  int
}

// post sends one JSON-RPC message and returns the result for its id (the server answers with
// JSON or with a short event stream).
func (c *Conn) post(msg map[string]any) map[string]any {
	req := must(http.NewRequest("POST", api+"/mcp", bytes.NewReader(must(json.Marshal(msg)))))
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set("mcp-protocol-version", "2025-06-18")
	if c.session != "" {
		req.Header.Set("mcp-session-id", c.session)
	}
	res := must(httpc.Do(req))
	defer res.Body.Close()
	if s := res.Header.Get("mcp-session-id"); s != "" {
		c.session = s
	}
	if res.StatusCode == http.StatusAccepted { // a notification: nothing comes back
		return nil
	}
	if res.StatusCode != http.StatusOK {
		log.Fatalf("MCP HTTP %d: %s", res.StatusCode, must(io.ReadAll(res.Body)))
	}
	var payloads [][]byte
	if strings.HasPrefix(res.Header.Get("content-type"), "text/event-stream") {
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 8<<20)
		for sc.Scan() {
			if line := sc.Text(); strings.HasPrefix(line, "data:") {
				payloads = append(payloads, []byte(strings.TrimSpace(line[5:])))
			}
		}
	} else {
		payloads = append(payloads, must(io.ReadAll(res.Body)))
	}
	for _, p := range payloads {
		var m map[string]any
		if json.Unmarshal(p, &m) == nil && m["id"] != nil && fmt.Sprint(m["id"]) == fmt.Sprint(msg["id"]) {
			if e, ok := m["error"]; ok {
				log.Fatalf("JSON-RPC error: %v", e)
			}
			return m["result"].(map[string]any)
		}
	}
	log.Fatalf("no response for request %v", msg["id"])
	return nil
}

func (c *Conn) request(method string, params map[string]any) map[string]any {
	c.nextID++
	return c.post(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
}

// terminate ends the MCP session (DELETE), as a client does when it's done.
func (c *Conn) terminate() {
	req := must(http.NewRequest("DELETE", api+"/mcp", nil))
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("mcp-session-id", c.session)
	if res, err := httpc.Do(req); err == nil {
		res.Body.Close()
	}
}

func agent() map[string]any {
	c := &Conn{}
	c.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "cdpfleet-case-82", "version": "1.0.0"},
	})
	c.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	calls := []map[string]any{}
	call := func(name string, args map[string]any) string {
		t := time.Now()
		r := c.request("tools/call", map[string]any{"name": name, "arguments": args})
		var parts []string
		for _, item := range r["content"].([]any) {
			if m := item.(map[string]any); m["type"] == "text" {
				parts = append(parts, m["text"].(string))
			}
		}
		text := strings.Join(parts, "\n")
		isErr, _ := r["isError"].(bool)
		first := strings.SplitN(text, "\n", 2)[0]
		if len(first) > 160 {
			first = first[:160]
		}
		calls = append(calls, map[string]any{"tool": name, "ms": time.Since(t).Milliseconds(), "error": isErr, "text": first})
		return text
	}

	launched := call("launch_browser", map[string]any{"browser": "chromium", "headless": true, "proxy": proxy, "record": false, "downloads": true})
	sessionID := ""
	if m := regexp.MustCompile(`session ([0-9a-f-]{36})`).FindStringSubmatch(launched); m != nil {
		sessionID = m[1]
	}
	call("navigate", map[string]any{"url": "https://httpbingo.org/"})
	call("click", map[string]any{"role": "link", "name": "/bytes/:n"}) // the index page's example link: 1,024 random bytes
	call("navigate", map[string]any{"url": agentFile})                // a file link opened directly
	call("evaluate", map[string]any{"expression": "location.href"})
	time.Sleep(2 * time.Second) // an assistant's next call comes seconds later; let the downloads finish
	call("close_browser", map[string]any{})

	// Ask the downloads tool until both files are there.
	closed := time.Now()
	text, polls := "", 0
	for time.Since(closed) < 2*time.Minute {
		polls++
		text = call("downloads", map[string]any{})
		if strings.Count(text, " bytes, sha256 ") >= 2 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	toolMs := time.Since(closed).Milliseconds()
	c.terminate()

	// "<name> — <bytes> bytes, sha256 <hex>" and a private link on the next line.
	links := map[string]string{}
	toolFiles := []map[string]any{}
	for _, m := range regexp.MustCompile(`(?m)^(.+) — (\d+) bytes, sha256 ([0-9a-f]{64})\n {2}(https:\S+)`).FindAllStringSubmatch(text, -1) {
		var nb int
		fmt.Sscan(m[2], &nb)
		links[m[1]] = m[4]
		toolFiles = append(toolFiles, map[string]any{"name": m[1], "bytes": nb, "sha256": m[3], "link_host": must(url.Parse(m[4])).Host})
	}
	files, _ := listed(sessionID, 2, time.Now())
	kept := []map[string]any{}
	for _, cl := range calls {
		if cl["tool"] != "downloads" {
			kept = append(kept, cl)
		}
	}
	last := calls[len(calls)-1]
	last["polls"] = polls
	return map[string]any{
		"session_id_found": sessionID != "", "calls": append(kept, last), "tool_listed_after_ms": toolMs,
		"first_downloads_call": calls[len(kept)]["text"],
		"tool_files": toolFiles, "files": fetchKept(files, links),
	}
}

func main() {
	p := person()
	a := agent()
	out, _ := json.MarshalIndent(map[string]any{"language": "go", "person": p, "agent": a, "measured_at": time.Now().UTC().Format("2006-01-02")}, "", "  ")
	fmt.Println(string(out))
}
