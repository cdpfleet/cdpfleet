// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

const api = "https://cdpfleet.com"
const poolSize = 4

var key = os.Getenv("CDPFLEET_API_KEY")

func getJSON(path string, out any) error {
	req, _ := http.NewRequest("GET", api+path, nil)
	req.Header.Set("x-api-key", key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(out)
}

func launch(pw *playwright.Playwright) (string, playwright.Browser, error) {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new", "record": false})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return "", nil, fmt.Errorf("launch: %s %s", res.Status, msg)
	}
	var s struct {
		SessionID string `json:"sessionId"`
		WsURL     string `json:"wsUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return "", nil, err
	}
	b, err := pw.Chromium.Connect(s.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	return s.SessionID, b, err
}

// How a session ended, from the account API (it appears a few seconds after the end).
func endReason(id string) any {
	for i := 0; i < 30; i++ {
		var j struct {
			Sessions []struct {
				SessionID string  `json:"session_id"`
				EndedAt   *string `json:"ended_at"`
				EndReason string  `json:"end_reason"`
			} `json:"sessions"`
		}
		if getJSON("/v1/me/sessions?limit=50", &j) == nil {
			for _, s := range j.Sessions {
				if s.SessionID == id && s.EndedAt != nil {
					return s.EndReason
				}
			}
		}
		time.Sleep(3 * time.Second)
	}
	return nil
}

func load(page playwright.Page, url string) bool {
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := page.Goto(url, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(45000)}); err == nil {
			return true
		}
	}
	return false
}

func main() {
	// Twelve light, public catalogue pages: what a crawler works through.
	urls := make([]string, 12)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://books.toscrape.com/catalogue/page-%d.html", i+1)
	}
	var me struct {
		Subscription struct {
			Kind     string `json:"kind"`
			Features struct {
				MaxPages int `json:"max_pages"`
				MemoryMB int `json:"memory_mb"`
			} `json:"features"`
		} `json:"subscription"`
	}
	if err := getJSON("/v1/me", &me); err != nil {
		log.Fatal(err)
	}
	maxPages, memoryMB := me.Subscription.Features.MaxPages, me.Subscription.Features.MemoryMB
	plan := map[string]any{"kind": me.Subscription.Kind, "max_pages": maxPages, "memory_mb": memoryMB}

	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()

	// 1 + 2: tabs. A new tab per URL, never closed, runs into the tab cap; a small pool of reused
	// tabs works through the same list without ever getting near it.
	_, tabs, err := launch(pw)
	if err != nil {
		log.Fatal(err)
	}
	ctx, err := tabs.NewContext()
	if err != nil {
		log.Fatal(err)
	}
	var opened []playwright.Page
	var closedAt any
	for i := 0; i <= maxPages; i++ {
		// Over the cap the node closes the new tab at once: NewPage() fails, or the tab is already closed.
		p, err := ctx.NewPage()
		if err == nil {
			time.Sleep(1500 * time.Millisecond)
		}
		if err != nil || p.IsClosed() {
			closedAt = i + 1
			break
		}
		p.SetContent(fmt.Sprintf("<title>tab %d</title>", i+1))
		opened = append(opened, p)
	}
	title, _ := opened[0].Title()
	tabCap := map[string]any{"opened_before_cap": len(opened), "closed_at_tab": closedAt, "session_alive": tabs.IsConnected() && title == "tab 1"}
	for _, p := range opened {
		p.Close()
	}

	pages := make([]playwright.Page, poolSize)
	for i := range pages {
		if pages[i], err = ctx.NewPage(); err != nil {
			log.Fatal(err)
		}
	}
	loaded := 0
	for i := 0; i < len(urls); i += poolSize {
		var wg sync.WaitGroup
		var mu sync.Mutex
		for j := 0; j < poolSize && i+j < len(urls); j++ {
			wg.Add(1)
			go func(p playwright.Page, u string) {
				defer wg.Done()
				if load(p, u) {
					mu.Lock()
					loaded++
					mu.Unlock()
				}
			}(pages[j], urls[i+j])
		}
		wg.Wait()
	}
	// JS heap of a loaded catalogue page (Chromium's performance.memory), averaged over the pool.
	sum := 0.0
	for _, p := range pages {
		v, _ := p.Evaluate("() => (performance.memory && performance.memory.usedJSHeapSize) || 0")
		switch n := v.(type) {
		case int:
			sum += float64(n)
		case float64:
			sum += n
		}
	}
	heapMB := math.Round(sum/float64(len(pages))/1048576*10) / 10
	pool := map[string]any{"pool_size": poolSize, "urls": len(urls), "loaded": loaded, "open_tabs": len(ctx.Pages())}
	for _, p := range pages {
		p.Close()
	}
	tabs.Close()

	// 3: memory. Half the cap is fine; growing a page past the cap ends the session.
	memID, mem, err := launch(pw)
	if err != nil {
		log.Fatal(err)
	}
	mb, halfOK, cut := 0, false, false
	page, err := mem.NewPage()
	if err != nil {
		log.Fatal(err)
	}
	page.Evaluate("() => { window.hog = []; }")
	for mb < memoryMB+1024 {
		if _, err := page.Evaluate("() => { const a = new Uint8Array(128 << 20); for (let i = 0; i < a.length; i += 4096) a[i] = 1; window.hog.push(a); }"); err != nil {
			cut = true
			break
		}
		mb += 128
		if mb >= memoryMB/2 && !halfOK {
			halfOK = mem.IsConnected()
		}
	}
	mem.Close()
	memory := map[string]any{"half_cap_ok": halfOK, "cut": cut, "allocated_mb_at_cut": mb, "end_reason": endReason(memID)}

	out, _ := json.MarshalIndent(map[string]any{
		"measured_at": time.Now().UTC().Format("2006-01-02"), "plan": plan, "tab_cap": tabCap, "pool": pool,
		"js_heap_mb_per_tab": heapMB, "memory": memory,
	}, "", "  ")
	fmt.Println(string(out))
}
