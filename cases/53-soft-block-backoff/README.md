# Surviving a soft block: honour Retry-After, then move to another exit without losing the login

**Medium** · 2026-10-16 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/soft-block-backoff)

> Two crawlers against the same rate-limited site. The one that retries a 429 straight away is challenged and gets 3 of 8 pages. The one that waits as told and moves the session to another proxy when its exit is blocked gets all 8 — same browser, same cookies.

## The problem

Sites rarely block outright on the first request. They slow you down first — a `429 Too Many Requests` with a `Retry-After` — and only challenge or ban an address that ignores it. A crawler needs a policy: tell a rate limit from a block, wait when asked, and when one exit is burned, continue from another without starting over (and without logging in again). What does that look like on a remote browser, and what does ignoring the signal cost?

## What we used, and why

| What | Why |
|---|---|
| A stand-in rate limiter (`context.route`) | Real sites don't rate-limit on cue, so the site is simulated in front of real httpbin requests. Each request really goes out through the session's proxy (`route.fetch`), and the limiter decides by the IP httpbin saw: 3 pages in a burst, then one per 5 s (429 with `Retry-After`), at most 6 per IP. An IP that comes back early or exceeds the quota gets a challenge page from then on. |
| A response classifier | `429` is a rate limit (wait). `403`/`503` or challenge text means this IP is blocked (move). `200` is data. Anything else (a `502` from a flaky upstream) is transient: retry after a second. Those show up in the trail now and then. |
| `Retry-After`, with exponential backoff as a fallback | Wait exactly as long as the site asks; if it doesn't say, back off 1, 2, 4… seconds. |
| Live proxy swap (`POST /admin/session/proxy` on the session's router) | Moves the running session to another proxy — no relaunch. |
| A new context with `storageState` | New connections, so every request uses the new proxy ([live proxy swap](https://cdpfleet.com/docs/cases/live-proxy-swap)), while the cookies come along. |
| Sticky SOCKS5 proxies | One exit IP per proxy, so "this IP is blocked" means something. |

## How it works

1. Naive crawler: one session, sets a session cookie, fetches 8 pages; on a 429 it retries straight away (up to 3 times), on a block it skips the page.
2. Polite crawler: the same, but on a 429 it waits `Retry-After` seconds, and on a block it swaps the session's proxy, opens a new context with the old `storageState` and retries.
3. Both read the cookie back at the end and report, per page, the status trail and the exit IP that served it.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-16):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `SOCKS_PROXIES` (see [cases/README.md](../README.md#environment)).

## What we got

| Crawler | Page | What happened | Result | Served from |
|---|---|---|---|---|
| naive | 1 | 200 | ok | 203.0.113.1 |
| naive | 2 | 200 | ok | 203.0.113.1 |
| naive | 3 | 200 | ok | 203.0.113.1 |
| naive | 4 | 429 → 403 | gave up | — |
| naive | 5 | 403 | gave up | — |
| naive | 6 | 403 | gave up | — |
| naive | 7 | 403 | gave up | — |
| naive | 8 | 403 | gave up | — |
| polite | 1 | 200 | ok | 203.0.113.2 |
| polite | 2 | 200 | ok | 203.0.113.2 |
| polite | 3 | 200 | ok | 203.0.113.2 |
| polite | 4 | 429 → wait 5s → 200 | ok | 203.0.113.2 |
| polite | 5 | 429 → wait 5s → 200 | ok | 203.0.113.2 |
| polite | 6 | 502 → 429 → wait 5s → 200 | ok | 203.0.113.2 |
| polite | 7 | 403 → swap proxy → 200 | ok | 203.0.113.3 |
| polite | 8 | 200 | ok | 203.0.113.3 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Retrying a 429 straight away turns a slowdown into a block.** The naive crawler got its first burst of pages, then a 429; its immediate retry arrived before `Retry-After` and the IP was challenged for the rest of the run: **3 or 4 of 8 pages**.
- **Waiting as told costs seconds, not pages.** The polite crawler waited `Retry-After` after every 429, a few seconds each, and kept getting pages from the same exit until its quota ran out.
- **When an exit is burned, move — don't wait.** At its quota the first IP was challenged. The crawler swapped the session to another proxy and opened a new context, and the blocked page came back `200` from a different exit IP: **8 of 8 pages, two exits, one proxy swap**.
- **The login survived the move.** The session cookie set before the crawl was still there after the swap, because the new context started from the old `storageState`. Same browser and same session, with no relaunch.
- Tell the signals apart: a 429 asks you to slow down on this IP, while a challenge means this IP is done for now. Treating every non-200 as "retry" gets the first wrong, and treating every one as "swap" spends exits you didn't need to.
