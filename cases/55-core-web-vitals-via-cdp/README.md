# Core Web Vitals from a remote browser: LCP, CLS and TTFB in lab runs

**Easy** · 2026-10-17 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/core-web-vitals-via-cdp)

> Read LCP, CLS, FCP and TTFB from the page's performance timeline, plus Chromium's own counters over a CDP session — and see why TTFB through a proxy is mostly the proxy.

## The problem

QA and performance teams want Core Web Vitals in CI and in monitoring: did this release make the largest paint slower, does a late banner push the page around? Lighthouse does it locally, but a fleet browser can do it from many places at once, behind the proxy and country your users see. Which numbers can you read from a remote browser, how, and what do they actually measure?

## What we used, and why

| What | Why |
|---|---|
| `PerformanceObserver` with `buffered: true` | Run after the load, it still returns the `largest-contentful-paint` and `layout-shift` entries the page recorded earlier — no init script needed. |
| Navigation Timing | `responseStart` is TTFB; `connectEnd − domainLookupStart` is the time to reach the server (DNS, TCP, TLS — through the proxy), `responseStart − requestStart` the server's own wait. |
| `context.newCDPSession(page)` + `Performance.getMetrics` | Chromium's counters over the normal Playwright connection: script time, layout count, DOM nodes, JS heap. |
| A fresh context per page, 1280×800 | Cold cache and the same viewport every time, as a lab run should be. |
| A made-up shifting page via `context.route` | A banner that arrives 600 ms late and pushes the text down: a layout shift on purpose, served without a network. |

## How it works

1. Launch headless Chromium behind a proxy.
2. For each page: new context, CDP session, `Performance.enable`, load, wait 1.5 s for late content.
3. Read FCP, LCP (and its element), CLS and the TTFB breakdown in the page; read the CDP counters.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-17):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | TTFB (ms) | …connect (ms) | …server wait (ms) | FCP (ms) | LCP (ms) | LCP element | CLS | Load (ms) | Script (ms) | DOM nodes | JS heap (MB) |
|---|---|---|---|---|---|---|---|---|---|---|---|
| example.com | 445 | 295 | 150 | 480 | 592 | p | 0 | 582 | 3 | 60 | 1.1 |
| books.toscrape.com | 856 | 639 | 217 | 1348 | 1352 | img | 0.002 | 1566 | 18 | 1772 | 2 |
| Wikipedia article | 726 | 465 | 261 | 1124 | 1124 | p | 0.095 | 7510 | 171 | 9495 | 8.8 |
| shifting demo | 36 | 0 | 36 | 64 | 64 | p | 0.234 | 45 | 1 | 87 | 1.1 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Vitals are one `evaluate` away.** Observers with `buffered: true`, created after the load, still hand back the LCP and layout-shift entries, so the same few lines of page script work from all five languages — no init script, no Lighthouse.
- **Through a proxy, TTFB includes the proxy.** TTFB splits cleanly into reaching the server (DNS, TCP, TLS — via the proxy) and the server's own wait. Reaching it took 226–1,975 ms in the published runs, usually more than the server's 89–686 ms wait (57–82 % of TTFB there; one rerun had it the other way round). To catch a slower backend, track `responseStart − requestStart`, not TTFB, and compare runs from the same proxy and country.
- **CLS catches a late banner:** the made-up page scored 0.234 in every run (above 0.1, Google's "good" line). The real pages barely moved — except the Wikipedia article, which scored 0.001 in three runs and 0.095 and 0.343 in the other two: one lab run is not a verdict, repeat it and look at the 75th percentile, as field data does.
- **Numbers swing between runs, ranks don't.** The LCP element was always an image on books.toscrape.com and a paragraph on example.com and Wikipedia, but books.toscrape.com's LCP ranged 1.35–4.68 s across five runs on different machines and proxy paths.
- **CDP counters show bloat the network hides:** the Wikipedia article had ~9,400 DOM nodes (29,518 in one run, where a different variant of the page came back), 104–212 ms of script and 4.8–9.6 MB of JS heap, against ~1,700 nodes and ~20 ms on books.toscrape.com. A release that doubles these is worth a look whatever the network did.
- **Lab, not field.** These are synthetic loads — no user input (so no INP), the fleet's CPUs and your proxy's path. Use them for regressions and for "how does it load from Germany through a residential exit"; use real-user data for what users get. To slow the CPU down like a phone, see [case 56](https://cdpfleet.com/docs/cases/cpu-throttling-via-cdp); for slow networks, [case 36](https://cdpfleet.com/docs/cases/slow-network). The CDP part needs a Chromium-based browser.
