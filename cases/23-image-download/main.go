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
	"math"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

const pageURL = "https://books.toscrape.com/" // 20 cover images

// Fetch each image again from inside the page and hand the bytes over as base64.
const refetch = `urls => Promise.all(urls.map(async (url) => {
  const buf = await (await fetch(url, { cache: 'no-store' })).arrayBuffer();
  let s = ''; const b = new Uint8Array(buf); for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
  return { url, b64: btoa(s) };
}))`

type img struct {
	url   string
	bytes []byte
}

type row struct {
	Method        string  `json:"method"`
	Images        int     `json:"images"`
	ValidJpeg     int     `json:"valid_jpeg"`
	TotalKB       int     `json:"total_kb"`
	ExtraRequests int     `json:"extra_requests"`
	Seconds       float64 `json:"seconds"`
}

func launch(name string, options map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(options)
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+name+"/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("launch %s: %s %s", name, res.Status, msg)
	}
	var session map[string]any
	return session, json.NewDecoder(res.Body).Decode(&session)
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func isJpeg(b []byte) bool { return len(b) > 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff }

func summary(method string, files []img, seconds float64, extra int) row {
	total, valid := 0, 0
	for _, f := range files {
		total += len(f.bytes)
		if isJpeg(f.bytes) {
			valid++
		}
	}
	return row{Method: method, Images: len(files), ValidJpeg: valid, TotalKB: int(math.Floor(float64(total)/1024 + 0.5)), ExtraRequests: extra, Seconds: seconds}
}

func main() {
	session := must(launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"}))
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()

	// Way 1: keep the bytes the page downloads anyway — zero extra requests. The listener
	// only remembers the response; bodies are read after navigation.
	page := must(browser.NewPage())
	var mu sync.Mutex
	var imageResponses []playwright.Response
	page.OnResponse(func(r playwright.Response) {
		if r.Request().ResourceType() == "image" && r.Ok() {
			mu.Lock()
			imageResponses = append(imageResponses, r)
			mu.Unlock()
		}
	})
	t1 := time.Now()
	must(page.Goto(pageURL, playwright.PageGotoOptions{Timeout: playwright.Float(60000), WaitUntil: playwright.WaitUntilStateNetworkidle}))
	raw := must(page.Locator("article.product_pod img").EvaluateAll("imgs => imgs.map(i => i.currentSrc || i.src)"))
	covers := map[string]bool{}
	var coverList []string
	for _, u := range raw.([]any) {
		covers[u.(string)] = true
		coverList = append(coverList, u.(string))
	}
	var fromLoad []img
	mu.Lock()
	snapshot := append([]playwright.Response(nil), imageResponses...)
	mu.Unlock()
	for _, r := range snapshot {
		if !covers[r.URL()] {
			continue
		}
		if b, err := r.Body(); err == nil { // body gone (cache) → skip
			fromLoad = append(fromLoad, img{r.URL(), b})
		}
	}
	way1 := summary("capture responses while the page loads", fromLoad, float64(time.Since(t1).Milliseconds())/1000, 0)

	// Way 2: fetch each image again from inside the page (same cookies, proxy and headers).
	t2 := time.Now()
	again := must(page.Evaluate(refetch, coverList))
	var refetched []img
	for _, a := range again.([]any) {
		m := a.(map[string]any)
		refetched = append(refetched, img{m["url"].(string), must(base64.StdEncoding.DecodeString(m["b64"].(string)))})
	}
	way2 := summary("fetch() each image again in the page", refetched, float64(time.Since(t2).Milliseconds())/1000, len(coverList))

	out, _ := json.MarshalIndent([]row{way1, way2}, "", "  ")
	fmt.Println(string(out))
}
