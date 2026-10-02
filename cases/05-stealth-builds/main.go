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
	"net/http"
	"os"
	"sync"

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

// The checks a bot-detection script typically runs, as a page script (not page.evaluate),
// exactly as a website would run them.
const checks = `<script>
window.__checks = (async () => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const perm = await navigator.permissions.query({ name: 'notifications' });
  return {
    webdriver: navigator.webdriver,
    headless_in_ua: /Headless/.test(navigator.userAgent),
    window_chrome: typeof window.chrome === 'object',
    plugins: navigator.plugins.length,
    languages: navigator.languages.join(','),
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    notification_permission_mismatch: Notification.permission === 'denied' && perm.state === 'prompt',
    outer_minus_inner_height: outerHeight - innerHeight,
  };
})();
</script>`

func check(pw *playwright.Playwright, name string, headless bool) map[string]any {
	mode := map[bool]string{true: "headless", false: "headful"}[headless]
	fail := func(err error) map[string]any {
		return map[string]any{"build": name, "mode": mode, "error": err.Error()}
	}
	session, err := launch(name, map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": headless})
	if err != nil {
		return fail(err)
	}
	browser, err := pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return fail(err)
	}
	defer browser.Close()
	page, _ := browser.NewPage()
	// A real https origin: some APIs (permissions, WebGL info) behave differently on about:blank.
	page.Route("https://detect.example/", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html"), Body: checks})
	})
	if _, err := page.Goto("https://detect.example/"); err != nil {
		return fail(err)
	}
	v, err := page.Evaluate("window.__checks")
	if err != nil {
		return fail(err)
	}
	out := v.(map[string]any)
	out["build"], out["mode"], out["version"] = name, mode, browser.Version()
	return out
}

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()
	// Every build twice: headless (1 thread) and headful on a virtual display (2 threads).
	builds := []string{"chromium", "chrome", "patchright", "cloakbrowser"}
	results := make([]map[string]any, len(builds)*2)
	var wg sync.WaitGroup
	for i, b := range builds {
		for j, headless := range []bool{true, false} {
			wg.Add(1)
			go func(k int, b string, headless bool) { defer wg.Done(); results[k] = check(pw, b, headless) }(i*2+j, b, headless)
		}
	}
	wg.Wait()
	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
}
