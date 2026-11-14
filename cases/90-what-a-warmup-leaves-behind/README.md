# What a warmup job leaves behind: the cookies, storage and visit history in the profile it warmed

**Medium** · 2026-11-14 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/what-a-warmup-leaves-behind)

> Run a 150-second warmup job on three ordinary public sites into a new persistent profile, then read what it left: the pages it visited, the cookies it collected — by site, first- vs third-party, and lifetime — and the profile itself; then open a later session on that profile and compare what it starts with, and what its first requests send, with a fresh session. In five languages, with the bill.

## The problem

A browser that has been somewhere before doesn't look like one that hasn't: it has the site's cookies, its consent choice, its saved preferences in localStorage, and a history of pages read. A [warmup job](https://cdpfleet.com/docs/jobs#warmup) builds that up for you — a browser of yours browses a few sites the way a visitor would, on one IP, and keeps what they leave in a [persistent profile](https://cdpfleet.com/docs/profiles). But what exactly does it leave? How many cookies, from whom, for how long — and does a later session on that profile actually start with them, and send them? This case runs a short warmup on three ordinary public sites, reads everything the job and the profile hold, and compares what a later session on the profile sees with a fresh session. It describes what is stored and what a site receives; it is not about getting past anything.

## What we used, and why

| What | Why |
|---|---|
| `POST /v1/functions/warmup` | `{url: Wikipedia's Main Page, sites: [books.toscrape.com, theguardian.com/us], browser: "camoufox", profile: "case90-<lang>-<random>", duration: 600, launch: {headless: true, proxy: <a sticky US exit>}}` → `202 {job_id}`. A new profile name per run: the job creates it. `duration` is only a safety net; the program stops the job after 150 s. |
| `behavior` | `{scroll: true, mouse: true, typing: false, dwell_s: [4, 10], pages_per_site: [2, 4]}`: scroll, move the mouse, read each page 4–10 s, 2–4 pages per site before moving on. `typing: false` because a warmup otherwise types words from the page into a site's search box now and then — this case submits no forms at all. |
| `GET /v1/jobs/{id}` and `DELETE /v1/jobs/{id}` | Progress every 5 s (`visits`, `sites`, `challenges`, `cookies_at`); `DELETE` at 150 s → `stopping`, then `stopped`. |
| `GET /v1/jobs/{id}/results` | The visit history: one `{type: "visit", url, status, dwell_s, challenged?}` line per page, and the `{type: "cookies"}` set delivered at the end. |
| `GET /v1/jobs/{id}/cookies` | The cookies the warmup's browser held — after the end, the last set it delivered (`live: false`). Counted by site, first-party (a site the warmup opened as a page) vs third-party, session vs persistent, and by how long they have left. |
| `GET /v1/me/profiles` | The profile the job created: its browser (`engine`), `version`, size, and when it was saved. |
| A Camoufox session with `"profile": {"id": …, "mode": "readonly"}` | The later session: restores the profile and never saves it, so looking doesn't change what the warmup left. A profile belongs to the browser that made it, so this has to be Camoufox too. Its context is `browser.contexts()[0]`. |
| `page.goto("https://<site>/robots.txt")` | One small request to each warmed site, the first in that session. `response.request().allHeaders()` gives the `Cookie` header it carried — what the site receives from this browser (only cookie names are printed, never values). The plain-text page belongs to the site's origin and runs none of the site's scripts, so the localStorage and IndexedDB a page there finds are what the session started with. |
| `DELETE /v1/me/profiles/{name}` | Removes the profile at the end, in a `finally`, once nothing holds it. |

## How it works

1. Start the warmup on the three sites into a new profile, on a sticky US exit.
2. Follow its progress every 5 s; after 150 s, `DELETE` it and wait until it is `stopped`.
3. Read the job's results (the pages it visited) and its cookies; count them by site, party, lifetime and flags.
4. Wait for the profile to be saved and read its size and browser.
5. Launch Camoufox read-only on the profile: list the cookies it starts with and compare them with the job's; request each site's `/robots.txt` once, record the `Cookie` header it sent, and read the origin's localStorage and IndexedDB from that page.
6. Do the same in a fresh Camoufox session without a profile.
7. Read the bill from the session history, and delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-14):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| Step | What happened | Measured |
|---|---|---|
| Warmup | 202: Camoufox on en.wikipedia.org, books.toscrape.com, www.theguardian.com, stopped at 152.2 s → stopped 5.7 s later; 14 pages (theguardian.com 4, toscrape.com 4, wikipedia.org 6), 1 challenged, statuses 200 | 2026-11-14 |
| Visit history | en.wikipedia.org/wiki/Main_Page → en.wikipedia.org/wiki/Wikipedia:Be_bold → books.toscrape.com/ → books.toscrape.com/catalogue/soumission_998/index.html → www.theguardian.com/us → www.theguardian.com/food/food → en.wikipedia.org/wiki/Main_Page → en.wikipedia.org/wiki/Stade_Sylvio_Cator → books.toscrape.com/ → books.toscrape.com/catalogue/the-dirty-little-secrets-of-getting-your-dream-job_994/index.html → www.theguardian.com/us → www.theguardian.com/email-newsletters → en.wikipedia.org/wiki/Main_Page → en.wikipedia.org/wiki/Special:SpecialPages | 2026-11-14 |
| Job cookies | 216 cookies from 96 sites: first-party 31 (theguardian.com 24, toscrape.com 0, wikipedia.org 7), third-party 185 from 94 sites (top: wikimedia.org 11, qvdt3feo.com 6, contextweb.com 5, pubmatic.com 5, rubiconproject.com 5); 11 session cookies, 49 httpOnly; expiry <1 day 10, <30 days 21, <1 year 132, ≥1 year 42 (max 400 days) | 2026-11-14 |
| Profile | camoufox, version 1, 1552 KB, saved 1.1 s after the job ended | 2026-11-14 |
| Later session (profile, read-only) | started with 216 cookies (31 first-party, 185 third-party): 216 of the job's 216, session cookies 11 of 11 | 2026-11-14 |
| en.wikipedia.org | with the profile: Cookie header 7 (WMF-Last-Access, WMF-Last-Access-Global, WMF-DP, NetworkProbeLimit, …), localStorage 8 keys, IndexedDB 0 databases · fresh session: Cookie header 0, localStorage 0, IndexedDB 0 | 2026-11-14 |
| books.toscrape.com | with the profile: Cookie header 0, localStorage 0 keys, IndexedDB 0 databases · fresh session: Cookie header 0, localStorage 0, IndexedDB 0 | 2026-11-14 |
| www.theguardian.com | with the profile: Cookie header 23 (gu_client_ab_tests, gu_v2_mvt_id, GU_mvt_id, bwid, …), localStorage 65 keys, IndexedDB 3 databases · fresh session: Cookie header 0, localStorage 0, IndexedDB 0 | 2026-11-14 |
| Cost | warmup 154 thread-seconds for 157.8 s (own camoufox session, weight 1); later session 13.8 s, fresh session 12.5 s | 2026-11-14 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **A short warmup reads real pages.** On 2026-11-14 each of the five runs browsed 12–15 pages in ~150 s — Wikipedia's Main Page and a few articles, books.toscrape.com's front and book pages, the Guardian's US front and a section — switching sites every 2–4 pages, reading 4–10 s each. Every page answered 200 (two runs had one load with no status). Four runs counted no challenge; one counted one (`progress.challenges: 1`) and simply moved on — a warmup never solves anything. `DELETE` stopped the job in 1–6 s.
- **The visit history lives in the job, not where a page can see it.** Each visit is a result line `{type: "visit", url, status, dwell_s}`, so you get the exact path the warmup took. Browsers don't let pages read their history, so this case doesn't measure it in the profile; what a site can see on a later visit is cookies and storage.
- **Most of the cookies are not from the sites it visited.** `GET /v1/jobs/{id}/cookies` held 182–221 cookies from 77–96 sites per run. First-party: 24–32 — the Guardian 17–25, Wikipedia 7, books.toscrape.com none. The other 153–192 came from 75–94 third-party sites, nearly all ad and identity services loaded by the Guardian's pages (Wikipedia's own `wikimedia.org` counts as third-party here, 7–11). 96–99 % were `SameSite=None`, 39–55 `httpOnly`, 9–11 session cookies; 10–23 expire within a day, 33–42 last a year or more (up to 400 days). The count swings with the news site's ad auction, not with the warmup.
- **The profile was saved at once, for Camoufox.** The job created the profile (1.5–1.65 MB) and saved it 0.6–1.4 s after the job ended. A profile belongs to the browser that made it — the later session has to be Camoufox too.
- **A later session starts exactly where the warmup stopped.** In 5 of 5 runs a read-only session on the profile started with **all** of the job's cookies (182–221 of 182–221, the 9–11 session cookies included), and its first requests sent them: 7 cookies to Wikipedia, 17–23 to the Guardian, none to books.toscrape.com (which sets none). The pages found the sites' storage too — Wikipedia 4–8 localStorage keys, the Guardian 60–66 keys and 2–3 IndexedDB databases. A fresh session without the profile started with none of it. (On 2026-11-11 the profile came back empty because the warmup browsed in a separate, unsaved context; Functions fixed that in 77c21b4, live since 2026-11-14.)
- **Billed as one Camoufox session for the seconds its browser ran.** 147–154 thread-seconds for a 153–158 s job (the difference is the launch before the browser starts), the same as its own Camoufox session (weight 1) in the session history; a later look at the profile cost 13–16 s and the fresh session 13–17 s.
