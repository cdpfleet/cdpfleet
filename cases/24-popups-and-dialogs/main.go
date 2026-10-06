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

	"github.com/playwright-community/playwright-go"
)

var key = os.Getenv("CDPFLEET_API_KEY")

// A page with everything that interrupts a script: a new-tab link, window.open, and the
// three blocking dialogs. Served from the browser itself, so the case needs no third party.
const html = "<!doctype html><title>Interruptions</title>\n" +
	"<a id=\"blank\" href=\"https://example.com/\" target=\"_blank\">open in a new tab</a>\n" +
	"<button id=\"open\" onclick=\"window.open('https://example.com/?popup', 'pop', 'width=480,height=320')\">window.open</button>\n" +
	"<button id=\"alert\" onclick=\"alert('Saved!')\">alert</button>\n" +
	"<button id=\"confirm\" onclick=\"document.body.dataset.confirm = String(confirm('Delete 3 items?'))\">confirm</button>\n" +
	"<button id=\"prompt\" onclick=\"document.body.dataset.prompt = String(prompt('Your name?', 'anonymous'))\">prompt</button>"

type row struct {
	Event            string `json:"event"`
	WhatHappened     string `json:"what_happened"`
	HandledWith      string `json:"handled_with"`
	PagesOpen        int    `json:"pages_open"`
	OpenerIsMainPage *bool  `json:"opener_is_main_page"`
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func main() {
	body, _ := json.Marshal(map[string]any{"proxy": os.Getenv("PROXY_URL"), "headless": "new"})
	req, _ := http.NewRequest("POST", "https://starter.cdpfleet.com/chromium/session", bytes.NewReader(body))
	req.Header.Set("x-api-key", key)
	req.Header.Set("content-type", "application/json")
	res := must(http.DefaultClient.Do(req))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		log.Fatalf("launch %s %s", res.Status, msg)
	}
	var session map[string]any
	must(0, json.NewDecoder(res.Body).Decode(&session))

	pw := must(playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true}))
	defer pw.Stop()
	browser := must(pw.Chromium.Connect(session["wsUrl"].(string), playwright.BrowserTypeConnectOptions{Headers: map[string]string{"x-api-key": key}}))
	defer browser.Close()

	context := must(browser.NewContext())
	page := must(context.NewPage())
	must(0, page.SetContent(html))
	out := []row{}

	// New pages: listen on the context BEFORE the click, then wait for the popup to load.
	for _, target := range [][2]string{{"link with target=_blank", "#blank"}, {"window.open()", "#open"}} {
		selector := target[1]
		popup := must(context.ExpectPage(func() error { return page.Click(selector) }))
		must(0, popup.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: playwright.LoadStateLoad, Timeout: playwright.Float(60000)}))
		opener := must(popup.Opener()) == page
		out = append(out, row{target[0], "new page: " + popup.URL() + " — \"" + must(popup.Title()) + "\"",
			"context.waitForEvent(\"page\") + popup.waitForLoadState()", len(context.Pages()), &opener})
		must(0, popup.Close())
	}

	// Dialogs: without a handler Playwright dismisses them (confirm → false, prompt → null).
	must(0, page.Click("#confirm"))
	out = append(out, row{"confirm() with no dialog handler", fmt.Sprintf("page saw confirm() return %v", must(page.Evaluate("() => document.body.dataset.confirm"))),
		"nothing — auto-dismissed", len(context.Pages()), nil})

	// With a handler you decide: accept, dismiss, or type an answer.
	seen := []string{}
	page.OnDialog(func(d playwright.Dialog) {
		seen = append(seen, d.Type()+": \""+d.Message()+"\"")
		if d.Type() == "prompt" {
			d.Accept("Ada Lovelace")
		} else {
			d.Accept()
		}
	})
	must(0, page.Click("#alert"))
	must(0, page.Click("#confirm"))
	must(0, page.Click("#prompt"))
	results := must(page.Evaluate("() => ({ confirm: document.body.dataset.confirm, prompt: document.body.dataset.prompt })")).(map[string]any)
	out = append(out, row{"alert()", seen[0], "dialog.accept()", len(context.Pages()), nil})
	out = append(out, row{"confirm() with a handler", fmt.Sprintf("%s → page saw %v", seen[1], results["confirm"]), "dialog.accept()", len(context.Pages()), nil})
	out = append(out, row{"prompt()", fmt.Sprintf("%s → page saw \"%v\"", seen[2], results["prompt"]), "dialog.accept(\"Ada Lovelace\")", len(context.Pages()), nil})

	text, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(text))
}
