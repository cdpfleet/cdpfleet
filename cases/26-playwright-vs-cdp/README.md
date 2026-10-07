# Same Chrome, two doors: Playwright's protocol (wsUrl) or CDP (cdpUrl)

**Medium** · 2026-10-07 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/playwright-vs-cdp)

> One Chrome launched twice — driven once through Playwright's own protocol and once over the Chrome DevTools Protocol — and what changes for your code and for the page.

## The problem

cdpfleet sessions can now be driven two ways. `wsUrl` speaks Playwright's own protocol: you need a Playwright client of the server's exact minor version, and you get everything Playwright offers. With `"cdp": true` you also get `cdpUrl`, a plain Chrome DevTools Protocol endpoint: Puppeteer, `connectOverCDP` in any Playwright version, chromedp, Rod and most agent frameworks connect there. Which should you use — is one faster, does one lose features, and does one give the automation away to the page?

## What we used, and why

| What | Why |
|---|---|
| `chrome` with `"cdp": true` | Real Google Chrome; the launch response then carries `cdpUrl` next to `wsUrl`. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp). |
| `chromium.connect(wsUrl)` | Playwright's own protocol. The client must match the server's minor version (1.60). |
| `chromium.connectOverCDP(cdpUrl)` | Playwright on top of plain CDP — any recent client version works, in every language. |
| `page.on("console")`, `page.pdf()` | Two features that depend on what the connection carries: console events and Chrome's PDF printer. |
| The stack-getter probe | A page logs an error whose `stack` getter records whether a debugger read it — the classic tell of a CDP client that has `Runtime.enable`d the page. |

## How it works

1. Launch Chrome with `"cdp": true` and connect with `connect(wsUrl)`; open example.com, listen for console messages, run the probe, try `page.pdf()`, close.
2. Launch again and do the same through `connectOverCDP(cdpUrl)`.
3. Print one row per connection: connect time, browser version, console events, PDF, what the probe saw, `navigator.webdriver`.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-07):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Connection | Endpoint | Client version must match | Connect (ms) | Browser | Console events | PDF | Stack read by debugger | navigator.webdriver |
|---|---|---|---|---|---|---|---|---|
| playwright protocol | wsUrl | true | 32 | 155.0.8059.39 | true | true | false | false |
| connectOverCDP | cdpUrl | false | 52 | 155.0.8059.39 | true | true | false | false |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Same browser, same results:** both connections reached the same Chrome build, got console events, printed a PDF and connected in a few dozen milliseconds. For most code the choice is about the client, not the browser.
- **`connectOverCDP` frees you from version matching:** `connect()` needs Playwright 1.60 exactly; over CDP any recent client works — and so do Puppeteer, chromedp, Rod and agent frameworks that only speak CDP.
- **Neither tripped the stack-getter probe here,** and `navigator.webdriver` was `false` both ways. (Puppeteer, measured separately on the same page, didn't trip it either, with or without a console listener.) Detection scripts look at more than this one signal — see [headless tells](https://cdpfleet.com/docs/cases/headless-tells) and [stealth builds](https://cdpfleet.com/docs/cases/stealth-builds).
- **Use `wsUrl` with the stealth builds:** Patchright and Rebrowser keep their patches in Playwright's layer, which is why they refuse `"cdp": true`.
- **Closing either ends the session:** you can connect to both at once (Playwright and Puppeteer on one Chrome), but disconnecting one closes the other.
