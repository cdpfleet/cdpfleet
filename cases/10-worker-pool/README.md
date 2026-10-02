# A thread-aware worker pool: one session per page vs. reusing sessions

**Hard** · 2026-09-30 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/worker-pool)

> 24 pages on 6 parallel workers, two strategies, with the retries the API asks for. Reusing sessions is faster and costs a fraction of the thread time.

## The problem

The simplest scraper launches a browser per URL. At scale that means paying the launch every time and holding a thread while the browser starts and stops. The alternative is a pool of long-lived sessions. How big is the difference — and how do you size the pool and handle the API's back-pressure properly?

## What we used, and why

| What | Why |
|---|---|
| `GET https://cdpfleet.com/v1/me` | Reads your plan, so the pool never has more workers than you have threads. |
| Launch retries | `429` (thread limit, launch rate) and `503` (fleet momentarily busy) carry `Retry-After`; wait that long and retry. Quota errors (`daily_quota_exhausted`, `hourly_quota_exhausted`) are not retried. |
| Page retries | Residential proxies occasionally drop a tunnel (`ERR_TUNNEL_CONNECTION_FAILED`): retry the navigation, up to three times. |
| Relaunch on a failed connect | Rarely, the server holding a new browser doesn't answer the WebSocket (`502 upstream_unreachable`). Don't reconnect to the same `wsUrl` — launch a fresh session. The failed one isn't billed. |
| `headless: true` | One thread per worker — the cheapest mode for plain page loads. |
| Wikipedia `Special:Random` | A cheap, always-different page for the workload. |

## How it works

1. Size the pool: `min(threads, 6)` workers.
2. Strategy A: every page gets its own session (launch → connect → load → close).
3. Strategy B: every worker opens one session and loads pages until the queue is empty.
4. Compare wall time, launches and billed thread-seconds (the time sessions were open).

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Strategy | Pages | Wall time (s) | Launches | Launch (ms) | Billed thread-s | Connect failures | Page retries |
|---|---|---|---|---|---|---|---|
| new session per page | 24 | 49.6 | 24 | 310 | 177 | 0 | 0 |
| reuse one session per worker | 24 | 23 | 6 | 780 | 106 | 0 | 0 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Reusing sessions billed a third to half of the thread time** for the same 24 pages, and in clean runs finished in about half the wall time. A single slow proxy tunnel can dominate either strategy — that's what `page_retries` is for.
- **Launching is quick (~0.3–0.5 s) — the cost is everything around it:** connecting, the first navigation on a cold browser, and tearing it down, all of which you pay per page in strategy A.
- **Size from the plan, not a constant:** a pool larger than your threads just collects `429 threads_exceeded`.
- **Retries are part of the design, not an afterthought:** honour `Retry-After` for launches, relaunch on a failed connect, retry navigations on proxy errors — and count all three so a slow run explains itself.
- Start a fresh session when you need a fresh identity (new cookies, fingerprint or proxy) — otherwise reuse.
