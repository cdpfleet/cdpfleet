// go get github.com/playwright-community/playwright-go@v0.6000.0
// Driver: build playwright-core 1.60.0 from npm and set PLAYWRIGHT_DRIVER_PATH (see /docs/quickstart).
// env: CDPFLEET_API_KEY, PROXY_URL
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// A small page on httpbin.org's origin with an upload form and a download link.
const pageHTML = `<form method="post" action="/anything" enctype="multipart/form-data">
  <input type="file" name="upload" id="file"><button id="send">Send</button></form>
<a id="data" href="/bytes/102400?seed=42" download="data.bin">data</a>`

func main() {
	// A local file to upload: 2,000 CSV rows (~60 KB).
	uploadPath := filepath.Join(os.TempDir(), "cdpfleet-upload.csv")
	var csv strings.Builder
	csv.WriteString("id,token\n")
	for i := 1; i <= 2000; i++ {
		token := make([]byte, 12)
		rand.Read(token)
		fmt.Fprintf(&csv, "%d,%s\n", i, hex.EncodeToString(token))
	}
	must(0, os.WriteFile(uploadPath, []byte(csv.String()), 0o600))
	local := must(os.ReadFile(uploadPath))

	session := must(launch("chromium", map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": true}))
	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()

	page := must(browser.NewPage())
	must(0, page.Route("https://httpbin.org/files-demo", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html"), Body: pageHTML})
	}))
	goTo := func() {
		must(page.Goto("https://httpbin.org/files-demo", playwright.PageGotoOptions{Timeout: playwright.Float(60000)}))
	}
	goTo()

	// Upload: SetInputFiles reads the file HERE and streams it to the remote browser.
	t := time.Now()
	must(0, page.SetInputFiles("#file", uploadPath))
	answer := must(page.ExpectNavigation(func() error { return page.Click("#send") }, playwright.PageExpectNavigationOptions{Timeout: playwright.Float(60000)}))
	var echo struct {
		Files struct{ Upload string } `json:"files"`
	}
	must(0, answer.JSON(&echo))
	echoed := []byte(echo.Files.Upload)
	uploadMs := time.Since(t).Milliseconds()

	// Download: the file lands on the remote server; SaveAs streams it back here.
	goTo()
	t = time.Now()
	download := must(page.ExpectDownload(func() error { return page.Click("#data") }, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(60000)}))
	downloadPath := filepath.Join(os.TempDir(), download.SuggestedFilename())
	must(0, download.SaveAs(downloadPath))
	downloadMs := time.Since(t).Milliseconds()
	got := must(os.ReadFile(downloadPath))
	// The same seeded bytes fetched directly from here, to prove the copy is exact.
	res := must(http.Get("https://httpbin.org/bytes/102400?seed=42"))
	direct := must(io.ReadAll(res.Body))
	res.Body.Close()

	out, _ := json.MarshalIndent(map[string]any{
		"upload": map[string]any{"local_file": filepath.Base(uploadPath), "bytes": len(local), "sha256": sum(local),
			"server_received_bytes": len(echoed), "server_sha256": sum(echoed), "identical": sum(local) == sum(echoed), "ms": uploadMs},
		"download": map[string]any{"suggested_filename": download.SuggestedFilename(), "bytes": len(got), "sha256": sum(got),
			"direct_sha256": sum(direct), "identical": sum(got) == sum(direct), "ms": downloadMs},
	}, "", "  ")
	fmt.Println(string(out))
}
