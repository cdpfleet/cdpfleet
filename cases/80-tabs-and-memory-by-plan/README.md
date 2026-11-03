# How many tabs fit in one session, by plan, browser and "memory"

**Easy** · 2026-11-03 · Chromium, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/tabs-and-memory-by-plan)

> A long Wikipedia article opened in one new tab after another until the session ends: on Community (2 GB), a shared plan (4 GB) and a dedicated thread (6 GB), in Chromium, Firefox and WebKit, and with `"memory": 2`. How many tabs fit, what the client sees when the memory runs out, what a 51st tab gets, and what each run is billed.

## The problem

A crawler that keeps tabs open, an agent that never closes what it opened, a test that runs many pages in one browser: sooner or later the session runs into its limits. Every session can have up to 50 tabs, and the browser's memory is capped by the plan — 2 GB on Community, 4 GB on shared threads, 6 GB on a dedicated thread — unless you launch with `"memory": 2`, `3` or `4` for that many times the memory (and threads). This case measures how far those limits actually go with a real, mid-weight page, and what your code sees when it hits one.

## What we used, and why

| What | Why |
|---|---|
| A long Wikipedia article | A real, mid-weight page (text, images, a large DOM) that is fine to load repeatedly, one tab at a time. |
| One context, new tab per load | Each `newPage()` + `goto()` keeps the previous tabs open, as an agent or crawler that never closes tabs would. |
| `"memory": 2` | Twice the plan's memory for this session; the launch response shows the effective `memory_mb` and `weight`. |
| The error the client gets | When the memory cap is hit, the next call fails with the reason in its message. |
| `GET /v1/me/sessions` | The session's `end_reason`, `weight`, peak memory and duration: what was billed. |
| `GET /v1/me` | Which plan each key is on, so the runs are labelled without guessing. |

## How it works

1. For each run — Community Chromium; your plan with Chromium, Firefox, WebKit and Chromium with `"memory": 2`; a dedicated thread with Chromium — launch a headless session and note `memory_mb` and `weight` from the launch response.
2. Open the article in a new tab, again and again, until a call fails or 50 tabs are open.
3. At 50 tabs, try one more.
4. Close the session and read its record: end reason, weight, peak memory, duration.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-03):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `COMMUNITY_API_KEY`, `CDPFLEET_DEDICATED_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Plan · browser · memory | Memory cap · weight | Tabs opened | What ended it | Billed | Measured |
|---|---|---|---|---|---|
| Community · chromium | 2048 MB · weight 1 | 18 | session_memory_limit — the client saw "Session closed: session_memory_limit (browser ran out of memory: plan limit 2048 MB)" | 25 s × 1 = 25 thread-seconds | 2026-11-03 |
| Shared · chromium | 4096 MB · weight 1 | 36 | session_memory_limit — the client saw "Session closed: session_memory_limit (browser ran out of memory: plan limit 4096 MB)" | 27 s × 1 = 27 thread-seconds | 2026-11-03 |
| Shared · firefox | 4096 MB · weight 1 | 28 | session_memory_limit — the client saw "Session closed: session_memory_limit (browser ran out of memory: plan limit 4096 MB)" | 46 s × 1 = 46 thread-seconds | 2026-11-03 |
| Shared · webkit | 4096 MB · weight 1 | 18 | session_memory_limit — the client saw "Session closed: session_memory_limit (browser ran out of memory: plan limit 4096 MB)" | 185 s × 1 = 185 thread-seconds | 2026-11-03 |
| Shared · chromium · "memory": 2 | 8192 MB · weight 2 | 50 (the cap; 51st: refused) | client_closed (closed by the client), peak 5246 MB | 49 s × 2 = 98 thread-seconds | 2026-11-03 |
| Dedicated · chromium | 6144 MB · weight 1 | 50 (the cap; 51st: refused) | client_closed (closed by the client), peak 5130 MB | 77 s × 1 = 77 thread-seconds | 2026-11-03 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Memory ends a session long before the tab cap — unless the plan has room to spare.** On 2026-11-03, with a long Wikipedia article in each tab, headless Chromium fit 16–18 tabs in Community's 2 GB and 35–39 in a shared plan's 4 GB; a dedicated thread's 6 GB and a shared session with `"memory": 2` (8 GB) both reached the 50-tab cap, peaking at about 5.4 GB.
- **Each engine fits a different number.** In the same 4 GB, Chromium fit 35–39 tabs, Firefox 26–28 and WebKit 18 — and WebKit was much the slowest, about 2 minutes for those 18 loads, so give it longer timeouts.
- **The client is told why.** In Node.js, Python, Java and C#, the call that crossed the limit failed with `Session closed: session_memory_limit (browser ran out of memory: plan limit 4096 MB)`, and the session's record says `session_memory_limit`. Everything in the session is gone at that point; open fewer tabs at once, close finished ones, or launch with `"memory"`.
- **Go isn't told the reason.** Node.js, Python, Java and C# got the `Session closed: session_memory_limit …` message; playwright-go 0.6000 only reported `Connection closed`. In Go, read the reason from the session's record (`GET /v1/me/sessions`, `end_reason`).
- **A 51st tab is refused, but not by name.** At 50 tabs, `newPage()` failed with a generic `Target page, context or browser has been closed`, while the session itself stayed connected and usable. Count your open tabs: 50 is the cap on every plan, whatever the memory.
- **`"memory": 2` is billed as two threads.** The `memory: 2` session reported `weight: 2` at launch and in its record, and its thread-seconds were twice its running time. Use it for the sessions that need it, not by default.
