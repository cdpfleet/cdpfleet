// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

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

func size(path string) int64 { return must(os.Stat(path)).Size() }

func row(artifact string, bytes any, detail string, screenshots, snapshots, network any, openWith any) map[string]any {
	return map[string]any{"artifact": artifact, "bytes": bytes, "detail": detail, "screenshots": screenshots,
		"snapshots": snapshots, "network_entries": network, "open_with": openWith}
}

func main() {
	dir := must(os.MkdirTemp("", "cdpfleet-replay-"))
	session := must(launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"}))
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))

	t := time.Now()
	// Video is recorded on the remote browser and fetched when the page closes; the trace is
	// assembled by the client from events and screenshots streamed over the same connection.
	context := must(browser.NewContext(playwright.BrowserNewContextOptions{
		RecordVideo: &playwright.RecordVideo{Dir: playwright.String(dir), Size: &playwright.Size{Width: 1280, Height: 720}},
		Viewport:    &playwright.Size{Width: 1280, Height: 720},
	}))
	must(0, context.Tracing().Start(playwright.TracingStartOptions{Screenshots: playwright.Bool(true), Snapshots: playwright.Bool(true)}))
	page := must(context.NewPage())
	video := page.Video() // take the handle while the page is open
	goTo := func(url string) { must(page.Goto(url, playwright.PageGotoOptions{Timeout: playwright.Float(60000)})) }
	goTo("https://example.com/")
	goTo("https://httpbin.org/forms/post")
	must(0, page.GetByLabel("Customer name").Fill("Ada Lovelace"))
	must(0, page.GetByLabel("Large").Check())
	must(0, page.GetByRole(*playwright.AriaRoleButton, playwright.PageGetByRoleOptions{Name: "Submit order"}).Click())
	must(0, page.WaitForURL("**/post", playwright.PageWaitForURLOptions{Timeout: playwright.Float(60000)})) // httpbin echoes the form as JSON
	must(page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filepath.Join(dir, "final.png"))}))
	must(0, context.Tracing().Stop(filepath.Join(dir, "trace.zip")))
	must(0, context.Close()) // finishes the video
	must(0, video.SaveAs(filepath.Join(dir, "session.webm")))
	must(0, browser.Close())
	seconds := time.Since(t).Seconds()

	var actions []string
	screenshots, snapshots, network := 0, 0, 0
	archive := must(zip.OpenReader(filepath.Join(dir, "trace.zip")))
	for _, f := range archive.File {
		if strings.HasPrefix(f.Name, "resources/") && strings.HasSuffix(f.Name, ".jpeg") {
			screenshots++
		}
		if !strings.HasSuffix(f.Name, ".trace") && !strings.HasSuffix(f.Name, ".network") {
			continue
		}
		rc := must(f.Open())
		text := string(must(io.ReadAll(rc)))
		rc.Close()
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.HasSuffix(f.Name, ".network") {
				network++
				continue
			}
			var ev map[string]any
			must(0, json.Unmarshal([]byte(line), &ev))
			if ev["type"] == "frame-snapshot" {
				snapshots++
			}
			if m, ok := ev["method"].(string); ok && ev["type"] == "before" && m != "" {
				actions = append(actions, m)
			}
		}
	}
	archive.Close()
	webm := must(os.ReadFile(filepath.Join(dir, "session.webm")))
	videoDetail := "not a WebM file"
	if len(webm) >= 4 && bytes.Equal(webm[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		videoDetail = "valid WebM (EBML header)"
	}

	out, _ := json.MarshalIndent([]map[string]any{
		row("trace.zip", size(filepath.Join(dir, "trace.zip")), fmt.Sprintf("%d actions: %s", len(actions), strings.Join(actions, ", ")),
			screenshots, snapshots, network, "npx playwright show-trace trace.zip"),
		row("session.webm", len(webm), videoDetail, nil, nil, nil, "any video player"),
		row("final.png", size(filepath.Join(dir, "final.png")), "screenshot after the last step", nil, nil, nil, "image viewer"),
		row("whole run", nil, fmt.Sprintf("%.1f s including recording and downloads", seconds), nil, nil, nil, nil),
	}, "", "  ")
	fmt.Println(string(out))
}
