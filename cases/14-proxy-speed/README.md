# What your proxy costs you in page speed

**Medium** · 2026-10-01 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/proxy-speed)

> The same page, cold, through three proxies: connection setup, time to first byte, largest paint and full load — measured by the browser itself.

## The problem

Slow scrapes are usually blamed on the browser, but most of the time goes into the network path through the proxy. How much does the proxy choice change the numbers that matter — and is "residential" always the slow one?

## What we used, and why

| What | Why |
|---|---|
| Three sessions in parallel | Random residential, residential in Germany, datacenter SOCKS5 — the same page through each. |
| A fresh context per load | Cold cache and new connections every time, so every load pays the full setup. |
| Navigation Timing (`performance.getEntriesByType('navigation')`) | Connection setup through the proxy tunnel, time to first byte, DOM ready and load, as the browser recorded them. |
| `PerformanceObserver` for `largest-contentful-paint` | When the main content appeared — closest to "the page is usable". |
| Median of 3 loads | Residential exits vary; the median smooths one-off spikes. |

## How it works

1. Launch one session per proxy, all at once.
2. Load the page three times per proxy, each in a new context, and read the timings in the page.
3. Report the median per proxy.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-01):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `SOCKS_PROXIES` (see [cases/README.md](../README.md#environment)).

## What we got

| Proxy | Connect (ms) | First byte (ms) | LCP (ms) | DOM ready (ms) | Load (ms) |
|---|---|---|---|---|---|
| residential (any country) | 1614 | 742 | 3384 | 3382 | 7309 |
| residential (Germany) | 340 | 201 | 948 | 970 | 1946 |
| datacenter SOCKS5 | 543 | 119 | 1152 | 1165 | 2441 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Location beats proxy type:** the residential exit in Germany — next to our servers and to the site's European CDN — was the fastest of all, faster than the datacenter proxy.
- **A random residential exit is the slow, unpredictable one:** connection setup and the full load took several times longer, because traffic crossed continents twice.
- **Connection setup dominates cold loads:** every new connection is a TCP + TLS handshake through the proxy; reusing sessions and contexts (see the worker-pool case) avoids paying it over and over.
- Pick proxy countries near your targets, and measure with your real pages — these numbers move with every exit.
