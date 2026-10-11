# Slow 3G on a remote browser: network throttling over a CDP session

**Easy** · 2026-10-11 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/slow-network)

> Throttle a remote Chromium to Fast 4G and Slow 3G — and take it offline — with a CDP session over the ordinary Playwright connection, then time how long the same page takes.

## The problem

A fleet browser sits on a fast data-centre link behind your proxy, so it never sees what a phone on a weak connection sees: late images, slow first paint, timeouts your code doesn't expect. Playwright has no throttling option of its own, but Chromium does — through the DevTools protocol. Can you reach it on a remote browser without switching to `cdpUrl`?

## What we used, and why

| What | Why |
|---|---|
| `context.newCDPSession(page)` | A raw CDP channel to the page over the normal Playwright connection (Chromium-based browsers) — no `cdp: true` needed. |
| `Network.emulateNetworkConditions` | Chrome DevTools' throttling: latency and up/down throughput, or offline. |
| Fast 4G and Slow 3G presets | The numbers DevTools uses: 150 ms / 9 Mbit/s, and 2,000 ms / 400 kbit/s. |
| A fresh context per run | No cache carried over, so every profile loads the whole page. |

## How it works

1. Launch Chromium and, for each profile, open a context and a CDP session for its page.
2. Apply the throttling, load books.toscrape.com, and time the HTML and the full load.
3. Finally switch a page offline and try to load it.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-11):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Network | HTML ready (ms) | Fully loaded (ms) |
|---|---|---|
| no throttling | 2719 | 3317 |
| Fast 4G | 1948 | 2230 |
| Slow 3G | 8752 | 15712 |
| Offline | net::ERR_INTERNET_DISCONNECTED |  |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **CDP works over the Playwright connection:** `newCDPSession` gives you DevTools-only features like throttling without launching with `cdp: true`.
- **Slow 3G turns seconds into many seconds:** the full load went from 2.6–6 s unthrottled to 14.5–16 s on Slow 3G across our five runs — set timeouts for the networks your users have, not for the fleet's. (Fast 4G was within the proxy's own jitter.)
- **Offline fails the way a dead connection does** (`net::ERR_INTERNET_DISCONNECTED`), so you can test your error handling without breaking the proxy.
- **Throttling is per page and Chromium-only:** Firefox, Camoufox and WebKit have no CDP session; slow those down with a throttling proxy instead.
