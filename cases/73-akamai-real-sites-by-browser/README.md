# Akamai on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-28 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser)

> Three real sites behind Akamai Bot Manager, chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, through one sticky residential exit per site, with the state of Akamai's _abck cookie. Measured on 2026-10-28 and again on 2026-11-20, same sites and setup; both dated results are below.

## The problem

Akamai's own site ([case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)) shows how its Bot Manager behaves, but every real site sets its own rules. This case takes three real sites behind Akamai from our sweep of production sites, one that let headless Chrome in (easy), one that only Camoufox passed (medium) and one that stopped Camoufox too (hard), and loads each once with every browser the fleet offers, plus a returning Camoufox profile, through one sticky residential exit per site, as [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser) and [case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser) did for Cloudflare and DataDome. It also reads Akamai's `_abck` cookie right after the page loads and 8 s later. The sites aren't named: they stand for a kind of site, and their rules can change any day. It is a dated measurement, not an evasion guide; for the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting), and for the whole picture [/bot-protection](https://cdpfleet.com/bot-protection).

## What we used, and why

| What | Why |
|---|---|
| Twelve browsers | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, Chromium headless and Patchright (all Chromium), Firefox, WebKit, and Camoufox (based on Firefox source; not affiliated with Mozilla). Rebrowser needs a Playwright 1.52 client, so it's left out of a program written for 1.60. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit per site, shared by every setup on that site, so a difference between rows is the browser. |
| One session at a time per site | Each site's setups run one after another; the three sites run side by side. |
| `page.evaluate()` after 8 s | Akamai's block page ("Access Denied", a reference number) or an error status with a short page is `blocked`; a press-and-hold or "verify you are human" page is `challenge`; anything else `content`. A connection Akamai resets (`ERR_HTTP2_PROTOCOL_ERROR`) counts as `blocked`. Nothing is clicked. |
| `context.cookies()` | The `_abck` cookie's status field (the second `~`-separated part) right after the page loads and 8 s later: `-1` while Akamai's sensor data hasn't validated the visitor, `0` once it has. Read only, never set. |
| `profile` | A returning visitor: Camoufox loads the site once with a new profile, the profile is saved, and a second session with it loads the site again. |
| `LAB_SITES_AKAMAI` | The three URLs come from the environment, so the published programs don't name the sites; set your own to repeat the measurement. |

## How it works

1. For each site, take one sticky residential exit and, one session at a time, open each of the twelve setups, load the page once, read `_abck`, wait 8 s and read it again.
2. Classify what the page shows and record the HTTP status of its first response; a network error gets one retry.
3. Then load the site with Camoufox and a new profile, wait for the profile to be saved, load it again with the same profile and record the second visit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-20):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a bank | Site B, a home-improvement retailer | Site C, a game-key marketplace | Measured |
|---|---|---|---|---|
| Chrome, headful | error | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| Chrome, headless | blocked (503) | blocked (200) | blocked | 2026-11-20 |
| Edge, headful | blocked (503) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| Brave, headful | blocked (503) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| Opera, headful | blocked (503) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-11-20 |
| Yandex, headful | blocked (503) | blocked (200) | blocked (200) · _abck -1 | 2026-11-20 |
| Whale, headful | blocked (503) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| Chromium, headless | blocked (503) | blocked (200) | blocked | 2026-11-20 |
| Patchright, headful | blocked (503) | content (200) · _abck -1 | blocked (403) · _abck -1 | 2026-11-20 |
| Firefox, headful | blocked (503) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| WebKit, headful | blocked (503) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-11-20 |
| Camoufox, headful | blocked (503) | content (200) · _abck -1 | blocked (403) · _abck -1 | 2026-11-20 |
| Camoufox, returning profile (2nd visit) | blocked (503) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-11-20 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Re-measured on 2026-11-20 (five runs, one per language), same three sites and setup as 2026-10-28.** The table shows the new run; the 2026-10-28 table is kept below it.
- **Headless was still refused on both protected sites.** Headless Chrome and headless Chromium got Akamai's block page on site B and were refused on site C in all five runs, as four weeks earlier; Yandex, though headful, was again blocked on site B in every run.
- **On the hardest site only Opera got in every time.** Site C let Opera in on all five runs on both dates. Brave, the other winner on 2026-10-28, was blocked there in all five runs this time, and WebKit got through once. Headful Chrome, Edge, Whale, Yandex, Patchright and Firefox were blocked in every run on both dates.
- **A returning Camoufox profile still did better than a fresh one, but less well.** On site C the fresh Camoufox was blocked in all five runs (it got in twice on 2026-10-28), and the second visit with a saved profile got the page in two of five (all five four weeks earlier).
- **`_abck` still never validated.** Wherever Akamai set `_abck`, its status read `-1` right after the page loaded and 8 s later, in every cell of every run, on both dates: the cookie's state didn't decide the page.
- **Site B was unchanged:** every other headful browser got it in every run (headful Chrome now in all five).
- **Site A had one bad run.** It let every setup in on four of five runs and set no `_abck` on its home page; in the fifth (the Node.js one) every setup got HTTP 503 with no Akamai cookie, the same answer whatever the browser. That reads as the site refusing that residential exit at that minute, not as a verdict on the browsers. The overview across vendors is [/bot-protection](https://cdpfleet.com/bot-protection); Cloudflare's and DataDome's real sites are [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser) and [case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser).
