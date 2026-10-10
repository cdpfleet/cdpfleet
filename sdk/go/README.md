# cdpfleet for Go

Launch real remote browsers on [cdpfleet](https://cdpfleet.com) and drive them with your own
[playwright-go](https://github.com/playwright-community/playwright-go). Full docs:
https://cdpfleet.com/docs/sdk

```sh
go get github.com/cdpfleet/cdpfleet/sdk/go
```

```go
package main

import (
	"fmt"
	"log"

	cdpfleet "github.com/cdpfleet/cdpfleet/sdk/go"
	"github.com/playwright-community/playwright-go"
)

func main() {
	pw, err := playwright.Run(&playwright.RunOptions{SkipInstallBrowsers: true})
	if err != nil {
		log.Fatal(err)
	}
	defer pw.Stop()

	fleet, err := cdpfleet.New() // reads CDPFLEET_API_KEY
	if err != nil {
		log.Fatal(err)
	}
	session, err := fleet.Launch("chrome", cdpfleet.Options{"proxy": "http://user:pass@host:port", "headless": true})
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	browser, err := session.Connect(pw.Chromium)
	if err != nil {
		log.Fatal(err)
	}
	page, _ := browser.NewPage()
	page.Goto("https://example.com")
	fmt.Println(page.Title())
}
```

`Launch` retries fleet backpressure (503) and rate limits (429) for up to `WithMaxWait`
(120 s); other failures return a `*cdpfleet.Error` with `Status`, `Code` and `Body`.

## keep_alive

Launch with `"keep_alive": true` and the browser outlives your connection: after a disconnect,
call `session.Connect(pw.Chromium)` again to re-attach to the same pages. `session.Close()`
then ends the session too (without `keep_alive` it only disconnects); `session.End()` ends it
explicitly.

## Driver

playwright-go v0.6000.0 needs the Playwright 1.60 driver: set `PLAYWRIGHT_DRIVER_PATH` to a
playwright-core 1.60 build and run with `SkipInstallBrowsers` (the browsers run on cdpfleet).
See https://cdpfleet.com/docs/quickstart.

Also: `ConnectOverCDP` (launch with `"cdp": true`), `LiveLink("view" | "control")`,
`SwapProxy`, and the account API — `Me`, `Usage`, `Threads`, `Sessions`, `Recordings`,
`RecordingURL`, `DeleteRecording`, `ProxyBalance`, `EndSession`.
