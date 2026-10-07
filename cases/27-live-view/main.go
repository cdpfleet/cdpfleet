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
	"path/filepath"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

type row struct {
	Browser              string  `json:"browser"`
	FramesWhileLoading   int     `json:"frames_while_loading"`
	FramesWhileScrolling int     `json:"frames_while_scrolling"`
	FpsWhileScrolling    float64 `json:"fps_while_scrolling"`
	FramesIn3sIdle       int     `json:"frames_in_3s_idle"`
	AvgKB                int     `json:"avg_kb"`
	AllJPEG              bool    `json:"all_jpeg"`
	Viewport             *string `json:"viewport"`
}

type errRow struct {
	Browser string `json:"browser"`
	Error   string `json:"error"`
}

type frame struct {
	phase string
	bytes int
	jpeg  bool
	w, h  int
	data  []byte
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func watch(pw *playwright.Playwright, engine string, headless any) any {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": headless})
	var res *http.Response
	for attempt := 1; attempt <= 5; attempt++ { // 503 = momentarily no capacity for this browser
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res = must(http.DefaultClient.Do(req))
		if res.StatusCode != 503 {
			break
		}
		res.Body.Close()
		time.Sleep(time.Duration(2*attempt) * time.Second)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		return errRow{Browser: engine, Error: fmt.Sprintf("launch %d %s", res.StatusCode, raw)}
	}
	var session struct {
		WsURL string `json:"wsUrl"`
	}
	must(0, json.Unmarshal(raw, &session))
	browserType := pw.Chromium
	if engine == "firefox" {
		browserType = pw.Firefox
	}
	browser := must(browserType.Connect(session.WsURL, playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()
	page := must(browser.NewPage())

	var mu sync.Mutex
	var frames []frame // every frame, tagged with the phase it arrived in
	phase := "load"
	screencast := must(page.Screencast())
	// Every frame arrives here as JPEG bytes — this is where a viewer would get it.
	must(0, screencast.Start(playwright.ScreencastStartOptions{
		Quality: playwright.Int(60),
		OnFrame: func(f playwright.OnFrame) {
			mu.Lock()
			defer mu.Unlock()
			jpeg := len(f.Data) > 1 && f.Data[0] == 0xff && f.Data[1] == 0xd8
			frames = append(frames, frame{phase: phase, bytes: len(f.Data), jpeg: jpeg, w: f.ViewportWidth, h: f.ViewportHeight, data: f.Data})
		},
	}))
	setPhase := func(p string) { mu.Lock(); phase = p; mu.Unlock() }
	for attempt := 1; ; attempt++ { // the proxy can drop a tunnel; retry
		_, err := page.Goto("https://en.wikipedia.org/wiki/Web_browser", playwright.PageGotoOptions{Timeout: playwright.Float(60000)})
		if err == nil {
			break
		}
		if attempt == 3 {
			log.Fatal(err)
		}
	}
	setPhase("scroll")
	t := time.Now()
	for i := 0; i < 10; i++ {
		must(0, page.Mouse().Wheel(0, 500))
		page.WaitForTimeout(400)
	}
	scrolling := time.Since(t).Seconds()
	setPhase("idle") // nothing changes on the page now
	page.WaitForTimeout(3000)
	must(0, screencast.Stop())

	mu.Lock()
	got := append([]frame(nil), frames...)
	mu.Unlock()
	count := func(p string) int {
		n := 0
		for _, f := range got {
			if f.phase == p {
				n++
			}
		}
		return n
	}
	out := row{Browser: engine, FramesWhileLoading: count("load"), FramesWhileScrolling: count("scroll"),
		FpsWhileScrolling: math.Round(float64(count("scroll"))/scrolling*10) / 10, FramesIn3sIdle: count("idle"), AllJPEG: true}
	total := 0
	for _, f := range got {
		total += f.bytes
		out.AllJPEG = out.AllJPEG && f.jpeg
	}
	out.AvgKB = int(math.Floor(float64(total)/math.Max(float64(len(got)), 1)/1024 + 0.5))
	if len(got) > 0 {
		last := got[len(got)-1]
		vp := fmt.Sprintf("%dx%d", last.w, last.h)
		out.Viewport = &vp
		os.WriteFile(filepath.Join(os.TempDir(), "cdpfleet-live-"+engine+".jpg"), last.data, 0o644)
	}
	return out
}

func main() {
	opts := &playwright.RunOptions{SkipInstallBrowsers: true}
	pw := must(playwright.Run(opts))
	defer pw.Stop()
	out := []any{watch(pw, "chromium", "new"), watch(pw, "firefox", true)}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
