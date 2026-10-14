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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

type setup struct {
	label, engine, family string
	headless              bool
}

var setups = []setup{
	{"Chromium, headless", "chromium", "chromium", true},
	{"Chrome, headful", "chrome", "chromium", false},
	{"Firefox, headful", "firefox", "firefox", false},
	{"WebKit, headful", "webkit", "webkit", false},
}

// A made-up https origin served by Playwright's router: a secure context (the clipboard and
// file APIs need one) with no network traffic. The page has a file input and a button that
// opens the File System Access picker; both report what they received in data attributes.
const pageURL = "https://app.example.test/"
const html = `<html><body>
<input type="file" id="f" onchange="document.body.dataset.input = this.files[0].name + ':' + this.files[0].size">
<button id="b" onclick="showOpenFilePicker().then(async ([h]) => { const f = await h.getFile(); document.body.dataset.picked = f.name + ':' + f.size; }).catch((e) => { document.body.dataset.picked = 'error: ' + e.name; })">open</button>
</body></html>`

// Clipboard round trip inside the page; a permission prompt nobody answers would hang, so 3 s cap.
const clipboard = `(async () => {
  const cap = (p) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('timed out (prompt?)')), 3000))]);
  const out = {};
  try { await cap(navigator.clipboard.writeText('copied-' + Date.now())); out.write = 'ok'; } catch (e) { out.write = e.name === 'Error' ? e.message : e.name; }
  try { const t = await cap(navigator.clipboard.readText()); out.read = t.startsWith('copied-') ? 'ok' : 'empty'; } catch (e) { out.read = e.name === 'Error' ? e.message : e.name; }
  return out;
})()`

// The origin-private file system: a sandboxed folder per site, no picker, no permission.
const opfs = `(async () => {
  try {
    const root = await navigator.storage.getDirectory();
    const fh = await root.getFileHandle('note.txt', { create: true });
    const w = await fh.createWritable(); await w.write('hello opfs'); await w.close();
    return (await (await fh.getFile()).text()) === 'hello opfs' ? 'ok' : 'mismatch';
  } catch (e) { return 'no (' + e.name + ')'; }
})()`

// Stands in for the native picker: returns a handle to a file the script provides.
const pickerStub = `window.showOpenFilePicker = async () => [{
  kind: 'file', name: 'report.csv', getFile: async () => new File(['id,total\n1,42\n'], 'report.csv', { type: 'text/csv' }),
}];`

// Field order matches the other languages' output.
type result struct {
	Label                  string `json:"label"`
	ClipboardWriteDefault  string `json:"clipboard_write_default"`
	ClipboardReadDefault   string `json:"clipboard_read_default"`
	InputSetFiles          string `json:"input_set_files"`
	ShowOpenFilePicker     bool   `json:"show_open_file_picker"`
	PickerFilechooserEvent bool   `json:"picker_filechooser_event"`
	PrivateFileSystem      string `json:"private_file_system"`
	PickerStubbed          string `json:"picker_stubbed"`
	GrantClipboard         string `json:"grant_clipboard"`
	ClipboardWriteGranted  string `json:"clipboard_write_granted,omitempty"`
	ClipboardReadGranted   string `json:"clipboard_read_granted,omitempty"`
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
func launch(engine string, opts map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(opts)
	for attempt := 1; ; attempt++ {
		req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/"+engine+"/session", bytes.NewReader(body))
		req.Header.Set("x-api-key", key)
		req.Header.Set("content-type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 200 {
			var session map[string]any
			return session, json.Unmarshal(b, &session)
		}
		if res.StatusCode != 503 || attempt == 5 {
			return nil, fmt.Errorf("launch %s %d %s", engine, res.StatusCode, strings.TrimSpace(string(b)))
		}
		wait, err := strconv.Atoi(res.Header.Get("retry-after"))
		if err != nil {
			wait = 5
		}
		time.Sleep(time.Duration(wait) * time.Second)
	}
}

func serve(context playwright.BrowserContext) error {
	return context.Route(pageURL+"**", func(route playwright.Route) {
		route.Fulfill(playwright.RouteFulfillOptions{ContentType: playwright.String("text/html"), Body: html})
	})
}

func freshPage(browser playwright.Browser, initScript string) (playwright.Page, error) {
	context, err := browser.NewContext()
	if err != nil {
		return nil, err
	}
	if err := serve(context); err != nil {
		return nil, err
	}
	if initScript != "" {
		if err := context.AddInitScript(playwright.Script{Content: playwright.String(initScript)}); err != nil {
			return nil, err
		}
	}
	page, err := context.NewPage()
	if err != nil {
		return nil, err
	}
	_, err = page.Goto(pageURL)
	return page, err
}

func chooserAfterClick(page playwright.Page, selector string) playwright.FileChooser {
	fc, err := page.ExpectFileChooser(func() error { return page.Click(selector) }, playwright.PageExpectFileChooserOptions{Timeout: playwright.Float(3000)})
	if err != nil {
		return nil
	}
	return fc
}

func clip(page playwright.Page) (string, string, error) {
	v, err := page.Evaluate(clipboard)
	if err != nil {
		return "", "", err
	}
	m := v.(map[string]any)
	return m["write"].(string), m["read"].(string), nil
}

func evalString(page playwright.Page, expr string) (string, error) {
	v, err := page.Evaluate(expr)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(v), nil
}

var apiPrefix = regexp.MustCompile(`^[A-Za-z]+\.[A-Za-z]+: `)

func one(pw *playwright.Playwright, s setup) (result, error) {
	out := result{Label: s.label}
	session, err := launch(s.engine, map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": s.headless})
	if err != nil {
		return out, err
	}
	browserType := pw.Chromium
	switch s.family {
	case "firefox":
		browserType = pw.Firefox
	case "webkit":
		browserType = pw.WebKit
	}
	browser, err := browserType.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}})
	if err != nil {
		return out, err
	}
	defer browser.Close()

	// 1. Clipboard with no permissions granted.
	page, err := freshPage(browser, "")
	if err != nil {
		return out, err
	}
	if out.ClipboardWriteDefault, out.ClipboardReadDefault, err = clip(page); err != nil {
		return out, err
	}

	// 2. Files in the same page: <input type=file>, the File System Access picker, the private file system.
	out.InputSetFiles = "no filechooser event"
	if fc := chooserAfterClick(page, "#f"); fc != nil {
		if err := fc.SetFiles([]playwright.InputFile{{Name: "report.csv", MimeType: "text/csv", Buffer: []byte("id,total\n1,42\n")}}); err != nil {
			return out, err
		}
		if out.InputSetFiles, err = evalString(page, "() => document.body.dataset.input || 'nothing'"); err != nil {
			return out, err
		}
	}
	has, err := page.Evaluate("() => typeof window.showOpenFilePicker === 'function'")
	if err != nil {
		return out, err
	}
	out.ShowOpenFilePicker = has == true
	out.PickerFilechooserEvent = out.ShowOpenFilePicker && chooserAfterClick(page, "#b") != nil
	if out.PrivateFileSystem, err = evalString(page, opfs); err != nil {
		return out, err
	}
	page.Context().Close()

	// 3. The picker stubbed with an init script: the page's own code receives our file.
	if page, err = freshPage(browser, pickerStub); err != nil {
		return out, err
	}
	if err := page.Click("#b"); err != nil {
		return out, err
	}
	if _, err := page.WaitForFunction("() => document.body.dataset.picked", nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)}); err != nil {
		return out, err
	}
	if out.PickerStubbed, err = evalString(page, "() => document.body.dataset.picked"); err != nil {
		return out, err
	}
	page.Context().Close()

	// 4. Clipboard with Chromium's permission names granted. Some engines reject the names —
	// at GrantPermissions or when the next page opens.
	context, err := browser.NewContext()
	if err != nil {
		return out, err
	}
	if err := serve(context); err != nil {
		return out, err
	}
	grant := func() error {
		if err := context.GrantPermissions([]string{"clipboard-read", "clipboard-write"}, playwright.BrowserContextGrantPermissionsOptions{Origin: playwright.String(pageURL)}); err != nil {
			return err
		}
		p, err := context.NewPage()
		if err != nil {
			return err
		}
		if _, err := p.Goto(pageURL); err != nil {
			return err
		}
		out.GrantClipboard = "accepted"
		out.ClipboardWriteGranted, out.ClipboardReadGranted, err = clip(p)
		return err
	}
	if err := grant(); err != nil {
		msg := apiPrefix.ReplaceAllString(strings.Split(err.Error(), "\n")[0], "")
		if len(msg) > 80 {
			msg = msg[:80]
		}
		out.GrantClipboard = msg
	}
	return out, nil
}

// run returns errors instead of exiting, so deferred Close and Stop calls still run.
func run() error {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		return err
	}
	defer pw.Stop()
	results := []result{}
	for _, s := range setups {
		r, err := one(pw, s)
		if err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
		results = append(results, r)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{"results": results}); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
