# A slow phone on a fast server: CPU throttling over a CDP session

**Easy** · 2026-10-17 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cpu-throttling-via-cdp)

> Slow a remote Chromium's main thread 4× and 6× with Emulation.setCPUThrottlingRate, then time a JavaScript workload and a real page load — and see that it's per tab.

## The problem

Fleet browsers run on server CPUs, many times faster than the mid-range phone most of your users carry. A page that feels instant in a test can stall on that phone: long parsing, slow hydration, input that lags. Lighthouse's mobile run throttles the CPU 4× for exactly this reason. Can you do the same on a remote browser, and what does it slow down — and what not?

## What we used, and why

| What | Why |
|---|---|
| `context.newCDPSession(page)` | A raw CDP channel to the page over the normal Playwright connection (Chromium-based browsers) — no `cdp: true` needed. |
| `Emulation.setCPUThrottlingRate` | DevTools' CPU throttling: `rate: 4` makes the page's main thread run at a quarter of its speed. |
| A fixed JavaScript workload | Three million loop steps and a 200,000-item sort, timed with `performance.now()` inside the page; the same result every time (`check`), so only the speed changes. |
| Navigation Timing + `Performance.getMetrics` | Response received, DOMContentLoaded and load from the page; main-thread busy time (`TaskDuration`) from Chromium. |
| A Wikipedia article | A real page with a big DOM (~9,400 nodes) and scripts to parse. |

## How it works

1. Launch headless Chromium behind a proxy.
2. For rates 1×, 4× and 6×: new context, CDP session, set the rate, load the article, read its timings and main-thread time, run the workload. Then reload it and keep the shorter of the two parse times: parsing waits for blocking scripts, and a single slow download through the proxy once made the 1× parse look slower than the 6× one (2026-10-21).
3. Finally throttle one tab 6× and run the workload in it and in an unthrottled tab next to it.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-21):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| CPU slowdown | JS workload (ms) | HTML → DOMContentLoaded (ms) | Main-thread busy (ms) | HTML received (ms) | Load event (ms) |
|---|---|---|---|---|---|
| 1× | 119 | 112 | 558 | 441 | 4998 |
| 4× | 316 | 676 | 1040 | 2402 | 4677 |
| 6× | 484 | 395 | 2320 | 2782 | 5454 |
| 6×, one tab only | 490 (other tab: 130) |  |  |  |  |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One CDP call per page, no `cdp: true`:** `newCDPSession` over the ordinary Playwright connection reaches DevTools' CPU throttling, the same knob Lighthouse's mobile run turns.
- **The rate is honest for JavaScript:** the same workload (same result every time) ran 2.7–4.0× slower at 4× and 4.1–7.3× slower at 6× across the five runs (2026-10-21).
- **It slows the page's work, not its network.** At 6× the main thread was busy 3.7–5.8× longer while loading the article, and getting from the received HTML to DOMContentLoaded (the shorter of the first load and a reload, with the scripts cached) went from 13–112 ms to 395–857 ms. The load event didn't follow — 1.8–8.9 s at every rate, in no order — because downloading through the proxy dominated it and isn't throttled. Measure main-thread time and parse time, not the load event; for a slow phone on a slow network, add [network throttling (case 36)](https://cdpfleet.com/docs/cases/slow-network).
- **Throttling is per tab:** with one tab at 6×, the workload took 368–519 ms there and 65–137 ms in the tab next to it, in the same browser. Set the rate on every page you open, and you can run a throttled and a full-speed profile in one session.
- **Compare ratios, not raw times:** the unthrottled workload took 64–119 ms on whichever machine ran the session, so a 4× run on one machine and a 1× run on another don't compare. Take the baseline in the same session. Core Web Vitals from the same kind of run are in [case 55](https://cdpfleet.com/docs/cases/core-web-vitals-via-cdp); CDP sessions need a Chromium-based browser.
