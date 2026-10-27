# Stay under the tab and memory caps: reuse a few pages, close what you're done with

**Easy** · 2026-10-27 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/stay-under-the-caps)

> Every session has a tab cap and a memory cap. A new tab per URL runs into the first; one page that grows runs into the second. A small pool of reused tabs works through the same list with room to spare.

## The problem

A crawler that opens a new tab for every URL and never closes them works fine on a laptop until it doesn't. On cdpfleet each session has a tab cap and a memory cap that depend on the plan, so the same habit hits a wall sooner: what happens at the wall, and what pattern never gets near it?

## What we used, and why

| What | Why |
|---|---|
| `GET /v1/me` | Reads the plan's caps: `subscription.features.max_pages` and `memory_mb` (the shared plan used here: 10 tabs, 4,096 MB per session). |
| `context.newPage()` in a loop | Opens tabs without closing any, until one is refused. |
| A pool of 4 pages | Works through 12 catalogue pages reusing the same four tabs: four at a time in Node, C# and Go; one at a time through the same tabs with Python's sync API and Java. |
| `performance.memory.usedJSHeapSize` | Chromium's JavaScript heap per tab, after a page has loaded. |
| `GET /v1/me/sessions` | The session's `end_reason` when the memory cap ends it. |

## How it works

1. Read the plan's caps from `/v1/me`.
2. In one session, open tabs without closing them until the server refuses one; check the session still works.
3. Close them, then open a pool of 4 tabs and load 12 catalogue pages through it, four at a time.
4. Read the JavaScript heap of each pooled tab.
5. In a second session, grow one page's memory in 128 MB steps: half the cap, then past it, and read how the session ended.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-27):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Measured | Value |
|---|---|
| Plan caps | shared: 10 tabs, 4096 MB per session |
| Tabs open before one was refused | 10 |
| Refused tab | #11, session still working |
| Pool of reused tabs | 4 tabs, 12/12 pages loaded, 4 open at most |
| JS heap per loaded tab | 9.5 MB |
| Memory past the cap | cut at ~3968 MB, end_reason session_memory_limit |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The tab after the cap is refused, and the session carries on.** On the shared plan (10 tabs) the 11th tab was closed at once while the first ten kept working, in every run on 2026-10-27. Your code sees `newPage()` fail or the new tab already closed: treat that as "full", not as a crash.
- **A pool of 4 reused tabs loaded all 12 pages, and never more than 4 were open.** Reusing a tab with `goto` is the pattern that never meets the cap, and four at a time keeps most of the speed of parallel tabs ([case 30](https://cdpfleet.com/docs/cases/parallel-pages)).
- **A light page costs little; one heavy page is what reaches the memory cap.** A loaded catalogue tab used about 10 MB of JavaScript heap. Growing one page passed half the cap without trouble, and going past 4,096 MB ended the session with `session_memory_limit` (cut at about 4 GB) in every run.
- **Plan the work, not the tabs.** For many URLs use a fixed pool sized to what you load at once (and below the cap), close tabs you're done with, and split very heavy pages across sessions; for many sessions in parallel see [case 10](https://cdpfleet.com/docs/cases/worker-pool). The caps per plan are on [Sessions & billing](https://cdpfleet.com/docs/sessions).
