# Puppeteer on real Google Chrome: cdpUrl, raw CDP and request interception

**Easy** · 2026-10-13 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/puppeteer-on-real-chrome)

> Drive real Chrome on cdpfleet with Puppeteer (or any CDP client): what the browser reports, raw CDP calls, blocking images, and closing the session.

## The problem

A lot of browser automation is written for Puppeteer or speaks the Chrome DevTools Protocol directly. With `"cdp": true`, a cdpfleet launch returns a `cdpUrl` for exactly those clients — but is the browser on the other end really Google Chrome, can you send raw CDP commands, does Puppeteer's request interception work through it, and what happens to the session when the client closes? [Case 26](https://cdpfleet.com/docs/cases/playwright-vs-cdp) compared Playwright's own protocol with CDP; this one is about using CDP the way Puppeteer users do.

## What we used, and why

| What | Why |
|---|---|
| `chrome` with `"cdp": true` | Real Google Chrome, headless; the launch response carries `cdpUrl` next to `wsUrl`. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp). |
| Node: `puppeteer.connect({ browserWSEndpoint: cdpUrl, headers })` | Puppeteer (`puppeteer-core` 24, no browser download). Its version doesn't have to match anything on our side. |
| Python, Java, C#, Go: `connectOverCDP(cdpUrl)` | These languages have no maintained Puppeteer, so they use Playwright's CDP client plus raw CDP sessions (`newBrowserCDPSession`, `newCDPSession`) for the same commands. Go's chromedp and Rod connect to `cdpUrl` too. |
| `Browser.getVersion`, `Target.getTargets` | Raw CDP at the browser level: the product string, the protocol version and the page targets. |
| Request interception | Puppeteer's `setRequestInterception` / Playwright's `route`: abort every image request on books.toscrape.com and compare the bytes the page pulled, from its own resource timing, each load in a fresh context. |
| `Performance.getMetrics` | Page metrics over raw CDP — Puppeteer wraps it as `page.metrics()`. |
| `GET /v1/me/threads` | After the client closes, how long until the session is gone from the live list. |

## How it works

1. Launch Chrome with `"cdp": true` and connect to `cdpUrl`.
2. Ask the browser for its version and count its page targets over raw CDP.
3. Load books.toscrape.com in a fresh context; load it again in another fresh context with images aborted; count targets again.
4. Read the first page's performance metrics over CDP, then close the client and watch the session disappear.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-13):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Measured | Value |
|---|---|
| Client | Puppeteer (puppeteer-core 24) |
| Connect | 202 ms |
| Browser.getVersion | Chrome/155.0.8059.39 (protocol 1.3) |
| Page targets | 0 → 2 |
| Load: everything | 331 KB in 4280 ms |
| Load: images blocked | 152 KB in 1561 ms, 32 images aborted |
| Performance.getMetrics | 1640 DOM nodes, 1.2 MB JS heap |
| Session gone after close | 0.1 s |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **It's real Google Chrome:** `Browser.getVersion` answered `Chrome/155.0.8059.39` (protocol 1.3) for Puppeteer and for Playwright's CDP client alike — not `HeadlessChrome`, even headless.
- **Connecting is quick:** 0.07–0.26 s from the launch response to a connected client across the five runs.
- **Request interception works over the connection:** aborting images cut books.toscrape.com from 331 KB to 152 KB transferred in every run — the same with Puppeteer and with Playwright's `route` (20–32 image requests aborted per run). See [bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet) for more ways to save proxy traffic.
- **Raw CDP is there when you need it:** browser-level (`Target.getTargets` saw the two pages appear) and page-level (`Performance.getMetrics`) commands both answered. Puppeteer exposes many as methods (`page.metrics()`); with Playwright you open a CDP session.
- **Closing the client ends the session:** the session left the live list 0.1–0.2 s after `browser.close()`, so billing stops there. Don't `disconnect()` and expect the browser to wait — use the [`keep_alive` option](https://cdpfleet.com/docs/browsers/chrome) for that (dedicated threads).
