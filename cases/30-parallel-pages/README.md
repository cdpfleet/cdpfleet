# Five pages at once: parallel tabs in one browser session

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/parallel-pages)

> Open five unrelated pages concurrently in one session and compare with loading them one by one — same thread, same proxy, a fraction of the wall time.

## The problem

You need something from several unrelated pages. The obvious loop opens one, extracts, closes, and moves on — and most of each wait is latency through the proxy while the browser sits idle. A session can hold many tabs, and each language has its own way to wait on several at once. How much does that save, and what does it cost?

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session (1 thread) for all five pages. |
| Five independent URLs | books.toscrape.com, quotes.toscrape.com, example.com, httpbin.org/html and scrapethissite.com: public, no login, safe to load at once. |
| `browser.newPage()` per URL | Each tab has its own navigation; all share the session's proxy and connection. |
| `Promise.all` · `asyncio.gather` · `Task.WhenAll` · goroutines | Each language's own way to wait on five loads at once. Java starts all five navigations and then waits for each — no threads. |
| `waitUntil: "domcontentloaded"` | Wait for the HTML, not every image and script: one slow asset would otherwise set the pace. |
| `page.title()` | The smallest proof a page loaded; put your extraction here. |

## How it works

1. Launch one Chromium session.
2. Sequential: open each URL in a new tab, read its title, close it — timed.
3. Parallel: open all five at once, read the titles, close the tabs — timed.
4. Print both timings, the speedup and every title.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Pages | Seconds |
|---|---|---|
| Sequential | 5 | 28.86 |
| Parallel | 5 | 4.26 |
| Speedup |  | 6.8x |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Tabs overlap the waiting:** 2–7× faster in our runs (11–29 s one by one, 2.4–6.6 s in parallel) — the wall time approaches the slowest page.
- **No extra threads or proxy slots:** five tabs are one session — the plan meter counts one thread.
- **The gain depends on page time, not page count:** fast pages gain little; slow ones through a distant proxy gain most.
- **Don't share Playwright across threads:** its Python sync API and its Java API aren't thread-safe (a thread pool fails with `Target closed` / `NoSuchElementException`). Use Python's async API, and in Java start every navigation before waiting on any.
- **Wait for the HTML, not the whole page:** with `load`, one page with a slow asset (18 s through our proxy) made the parallel run as slow as the sequential one.
- **Close tabs when done** — every open page holds memory in the remote browser.
