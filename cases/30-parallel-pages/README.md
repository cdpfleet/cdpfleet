# Five pages at once: extracting data in parallel within one browser session

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/parallel-pages)

> Open five pages concurrently in one browser session, extract a title from each, and compare with doing it one at a time. Same session, same proxy, 2.7x faster.

## The problem

When you need data from several unrelated pages, the default approach is sequential: open a page, extract, close, repeat. Each page load waits for the previous one to finish, and most of that wait is network latency through the proxy — time the browser spends idle. A single browser session can hold multiple pages open at once. If the pages are independent, there is no reason to serialize them.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session, five pages. |
| Five independent URLs | books.toscrape.com, quotes.toscrape.com, example.com, httpbin.org/html, scrapethissite.com — no auth, no overlap, safe to hit concurrently. |
| `browser.newPage()` per URL | Each page is an isolated tab with its own navigation state, but they share the session's proxy and connection. |
| `Promise.all` / `ThreadPoolExecutor` / `CompletableFuture` / `Task.WhenAll` / goroutines | Language-native concurrency — all five pages load at the same time. |
| `page.title()` | A minimal extraction to prove the page loaded; replace with any real scraping logic. |

## How it works

1. Launch one Chromium session.
2. **Sequential run:** open each of the 5 URLs in a new page one at a time, extract the title, close the page. Time the total.
3. **Parallel run:** open all 5 URLs concurrently, each in its own page, extract titles, close pages. Time the total.
4. Print both timings, the speedup ratio, and every result with its method label.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Pages | Seconds | Per page (s) |
|---|---|---|---|
| Sequential | 5 | 8.14 | 1.63 |
| Parallel | 5 | 2.97 | 0.59 |
| **Speedup** | | **2.7x** | |

Full output: [output.json](output.json).

## Takeaways

- **2.7x speedup from concurrent pages in one session** — the proxy and DNS latency that dominates sequential loads now overlaps instead of stacking.
- **One session, one proxy, multiple pages:** each `browser.newPage()` is an isolated tab sharing the same connection to the fleet and the same proxy exit. No extra sessions, no extra proxy slots.
- **The speedup scales with page-load time, not count:** five fast pages may only be 1.5x faster in parallel; five slow pages through a distant proxy can be close to 5x. The bottleneck is the slowest page, not the sum.
- **Thread safety varies by language:** Node.js is single-threaded but `Promise.all` overlaps I/O; Python's sync Playwright API needs real threads (`ThreadPoolExecutor`); Java uses `CompletableFuture`; C# uses `Task.WhenAll`; Go uses goroutines. The browser handles concurrency on its end regardless.
- **Close pages when done** — open tabs consume memory in the remote browser. Extract, close, move on.
