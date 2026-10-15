# A first free scrape on the Community plan: no proxy to bring, country and sticky exits included

**Easy** · 2026-10-15 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/community-first-scrape)

> On the free Community plan a launch without a proxy goes out through the plan's own shared proxy. Scrape a catalogue page, then pick a country and keep the same exit across sessions with a `cdpfleet-plan` token — and read the plan's limits from the API.

## The problem

Every cdpfleet session goes through a proxy, and on paid plans you bring your own. On the free Community plan the proxy is included: what happens when the launch names no proxy at all, how do you choose a country or keep the same IP, and what are the plan's limits — without guessing from the docs?

## What we used, and why

| What | Why |
|---|---|
| `GET /v1/me` | The plan as the account sees it: threads, daily minutes, session length, per-session page and memory limits, and which options the included proxy supports. |
| A launch without `proxy` | On Community the plan's included proxy is used. On a paid plan the same launch gets `400 proxy_required`. |
| `cdpfleet-plan-country-de-session-…-lifetime-10` | A token in place of a proxy URL: exit country Germany, and the same exit for 10 minutes for every session that uses the same session name. The dashboard's Proxies → Community proxy tab builds it. |
| `browser.close()` | Ends the session at once, so the 60 daily minutes per thread go further. |

## How it works

1. Read the plan from `GET /v1/me`.
2. Launch headless Chromium without a proxy, check the exit country, and scrape the 20 books on the first page of books.toscrape.com.
3. Launch twice more with the same `cdpfleet-plan` country + session token and compare the two exits.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-15):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `COMMUNITY_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Check | Result |
|---|---|
| Plan | community_60: 2 threads × 60 min/day, sessions ≤ 15 min, 5 pages / 3 GB per session |
| Included proxy options | country, session, lifetime |
| Launch without a proxy | weight 1 (headless), exit US |
| Scraped | 20 books, first: "A Light in the Attic" £51.77 |
| `cdpfleet-plan` country + session | DE then DE, same IP |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **No proxy needed:** the launch without `proxy` went out through the plan's shared proxy and scraped the page as usual. Leave `proxy` out on Community, or send your own proxy or a [residential](https://cdpfleet.com/docs/proxies#residential) token when you need one.
- **Country and sticky exits work on the free proxy:** the `cdpfleet-plan` token exited in Germany both times, from the same IP — a second browser that reuses the session name keeps the identity.
- **The limits, from the API:** 2 threads (a headless browser takes 1, a headful one 2), 60 browser-minutes per thread a day, sessions of up to 15 minutes, 5 open pages and 3 GB per session. The included proxy is shared by every Community user, so expect it to be slower at peak times.
- **What Community doesn't include:** session recordings, persistent profiles and `keep_alive`. The plan is invite-only for now — [join the waitlist](https://cdpfleet.com/#join) — and request data from Community sessions may be used to learn which browser setups work on which sites, so don't sign in to accounts on it ([Terms 4A](https://cdpfleet.com/terms)). Details: [Community plan](https://cdpfleet.com/docs/pricing#community).
