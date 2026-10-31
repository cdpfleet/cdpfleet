// No MCP SDK here: the official Go SDK needs Go 1.23+, so this speaks the protocol directly —
// JSON-RPC 2.0 over Streamable HTTP, with the standard library only.
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const mcpURL = "https://cdpfleet.com/mcp"
const extract = "() => [...document.querySelectorAll('article.product_pod')].map((a) => ({ title: a.querySelector('h3 a').title, price: a.querySelector('.price_color').textContent }))"

var key = os.Getenv("CDPFLEET_API_KEY")
var proxy = os.Getenv("PROXY_URL")
var httpc = &http.Client{Timeout: 120 * time.Second}

type Call struct {
	Tool             string `json:"tool"`
	Ms               int64  `json:"ms"`
	Error            bool   `json:"error"`
	WithInitializeMs *int64 `json:"with_initialize_ms,omitempty"`
}

// One MCP session. A script authenticates with its API key as a bearer token.
type Conn struct {
	session string
	nextID  int
}

// post sends one JSON-RPC message and returns the response for its id (the server answers
// with JSON or with a short event stream).
func (c *Conn) post(msg map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(msg)
	req, _ := http.NewRequest("POST", mcpURL, bytes.NewReader(body))
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set("mcp-protocol-version", "2025-06-18")
	if c.session != "" {
		req.Header.Set("mcp-session-id", c.session)
	}
	res, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if s := res.Header.Get("mcp-session-id"); s != "" {
		c.session = s
	}
	if res.StatusCode == http.StatusAccepted { // a notification: nothing comes back
		return nil, nil
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, b)
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
		b, _ := io.ReadAll(res.Body)
		payloads = append(payloads, b)
	}
	for _, p := range payloads {
		var m map[string]any
		if json.Unmarshal(p, &m) == nil && m["id"] != nil && fmt.Sprint(m["id"]) == fmt.Sprint(msg["id"]) {
			if e, ok := m["error"]; ok {
				return nil, fmt.Errorf("JSON-RPC error: %v", e)
			}
			return m["result"].(map[string]any), nil
		}
	}
	return nil, fmt.Errorf("no response for request %v", msg["id"])
}

func (c *Conn) request(method string, params map[string]any) (map[string]any, error) {
	c.nextID++
	return c.post(map[string]any{"jsonrpc": "2.0", "id": c.nextID, "method": method, "params": params})
}

func connect() (*Conn, error) {
	c := &Conn{}
	if _, err := c.request("initialize", map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "cdpfleet-case-79", "version": "1.0.0"},
	}); err != nil {
		return nil, err
	}
	_, err := c.post(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return c, err
}

// terminate ends the MCP session (DELETE), as a client does when it's done.
func (c *Conn) terminate() {
	req, _ := http.NewRequest("DELETE", mcpURL, nil)
	req.Header.Set("authorization", "Bearer "+key)
	req.Header.Set("mcp-session-id", c.session)
	if res, err := httpc.Do(req); err == nil {
		res.Body.Close()
	}
}

func (c *Conn) call(log *[]Call, name string, args map[string]any) (string, bool) {
	t := time.Now()
	r, err := c.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		panic(err)
	}
	var parts []string
	for _, item := range r["content"].([]any) {
		if m := item.(map[string]any); m["type"] == "text" {
			parts = append(parts, m["text"].(string))
		}
	}
	isErr, _ := r["isError"].(bool)
	*log = append(*log, Call{Tool: name, Ms: time.Since(t).Milliseconds(), Error: isErr})
	return strings.Join(parts, "\n"), isErr
}

// A shared proxy drops the odd connection: one retry of a failed page load.
func (c *Conn) navigate(log *[]Call, url string) (string, bool) {
	text, isErr := c.call(log, "navigate", map[string]any{"url": url})
	if isErr {
		return c.call(log, "navigate", map[string]any{"url": url})
	}
	return text, isErr
}

func firstLine(s string) string { return strings.SplitN(s, "\n", 2)[0] }

var launchedRe = regexp.MustCompile(`^Launched (\S+) (\S+) \((\w+), (\d+) thread`)
var sessionRe = regexp.MustCompile(`session ([0-9a-f-]{36})`)

// ---- 1. One MCP session for the whole job, as Claude or Cursor keep one.
func oneSession() map[string]any {
	log := []Call{}
	out := map[string]any{}
	t := time.Now()
	c, err := connect()
	if err != nil {
		panic(err)
	}
	log = append(log, Call{Tool: "initialize", Ms: time.Since(t).Milliseconds()})
	t = time.Now()
	list, err := c.request("tools/list", map[string]any{})
	if err != nil {
		panic(err)
	}
	log = append(log, Call{Tool: "tools/list", Ms: time.Since(t).Milliseconds()})
	names := []string{}
	for _, x := range list["tools"].([]any) {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	sort.Strings(names)
	out["mcp_session"] = c.session != ""
	out["tools"] = names

	text, _ := c.call(&log, "launch_browser", map[string]any{"browser": "chromium", "headless": true, "proxy": proxy, "record": false})
	if m := launchedRe.FindStringSubmatch(text); m != nil {
		w, _ := strconv.Atoi(m[4])
		out["browser"], out["version"], out["resource_class"], out["weight"] = m[1], m[2], m[3], w
	}
	sid := ""
	if m := sessionRe.FindStringSubmatch(text); m != nil {
		sid = m[1]
	}
	out["session_id"] = sid
	nav, _ := c.navigate(&log, "https://books.toscrape.com/")
	out["navigate"] = firstLine(nav)
	snap, _ := c.call(&log, "snapshot", map[string]any{})
	out["snapshot_chars"] = len([]rune(snap))
	snapList, _ := c.call(&log, "snapshot", map[string]any{"selector": "ol.row"})
	out["snapshot_list_chars"] = len([]rune(snapList))
	html, _ := c.call(&log, "evaluate", map[string]any{"expression": "document.documentElement.outerHTML.length"})
	out["html_chars"], _ = strconv.Atoi(strings.TrimSpace(html))
	booksText, _ := c.call(&log, "evaluate", map[string]any{"expression": extract})
	var books []map[string]string
	json.Unmarshal([]byte(booksText), &books)
	out["books"] = len(books)
	out["first_book"] = books[0]
	closed, _ := c.call(&log, "close_browser", map[string]any{})
	out["closed"] = strings.HasPrefix(closed, "Session "+sid+" ended")
	after, afterErr := c.call(&log, "navigate", map[string]any{"url": "https://books.toscrape.com/"})
	out["after_close"] = map[string]any{"error": afterErr, "text": firstLine(after)}
	acc, _ := c.call(&log, "account", map[string]any{})
	var account map[string]any
	json.Unmarshal([]byte(acc), &account)
	out["live_sessions_after"] = account["live_sessions"]
	c.terminate()
	out["calls"] = log
	return out
}

// ---- 2. A new MCP session for every tool call, as ChatGPT does.
func perCallSessions() map[string]any {
	log := []Call{}
	sessions := []string{}
	once := func(name string, args map[string]any) string {
		t := time.Now()
		c, err := connect()
		if err != nil {
			panic(err)
		}
		var text string
		if name == "navigate" {
			text, _ = c.navigate(&log, args["url"].(string))
		} else {
			text, _ = c.call(&log, name, args)
		}
		sessions = append(sessions, c.session) // no DELETE: the client just goes away
		ms := time.Since(t).Milliseconds()
		log[len(log)-1].WithInitializeMs = &ms
		return text
	}
	launched := once("launch_browser", map[string]any{"browser": "chromium", "headless": true, "proxy": proxy, "record": false})
	sid := ""
	if m := sessionRe.FindStringSubmatch(launched); m != nil {
		sid = m[1]
	}
	nav := once("navigate", map[string]any{"url": "https://books.toscrape.com/catalogue/page-2.html"})
	where := once("evaluate", map[string]any{"expression": `({ url: location.href, books: document.querySelectorAll("article.product_pod").length })`})
	closed := once("close_browser", map[string]any{})
	var sees map[string]any
	json.Unmarshal([]byte(where), &sees)
	distinct := map[string]bool{}
	for _, s := range sessions {
		distinct[s] = true
	}
	return map[string]any{
		"mcp_sessions":          len(sessions),
		"distinct_mcp_sessions": len(distinct),
		"browser_session_id":    sid,
		"navigate":              firstLine(nav),
		"later_session_sees":    sees,
		"closed_same_browser":   strings.HasPrefix(closed, "Session "+sid+" ended"),
		"calls":                 log,
	}
}

func main() {
	one := oneSession()
	perCall := perCallSessions()
	b, _ := json.MarshalIndent(map[string]any{"language": "go", "one_session": one, "per_call": perCall,
		"measured_at": time.Now().UTC().Format("2006-01-02")}, "", "  ")
	fmt.Println(string(b))
}
