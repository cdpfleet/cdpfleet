# DataDome on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-27 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser)

> Three real sites behind DataDome, chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, all through one sticky residential exit per site. The outcome per cell, dated 2026-10-27.

## The problem

DataDome's own site ([case 65](https://cdpfleet.com/docs/cases/datadome-what-gets-through)) shows how its checks behave, but every real site tunes them. This case takes three real sites behind DataDome from our sweep of production sites, one that let headless Chrome in (easy), one that only Camoufox passed (medium) and one that stopped Camoufox too (hard), and loads each once with every browser the fleet offers, plus a returning Camoufox profile, through one sticky residential exit per site, the same way [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser) did for Cloudflare. The sites aren't named: they stand for a kind of site, and their rules can change any day. It is a dated measurement, not an evasion guide; for the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting), and for the whole picture [/bot-protection](https://cdpfleet.com/bot-protection).

## What we used, and why

| What | Why |
|---|---|
| Twelve browsers | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, Chromium headless and Patchright (all Chromium), Firefox, WebKit, and Camoufox (based on Firefox source; not affiliated with Mozilla). Rebrowser needs a Playwright 1.52 client, so it's left out of a program written for 1.60. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit per site, shared by every setup on that site, so a difference between rows is the browser. |
| One session at a time per site | Each site's setups run one after another; the three sites run side by side. |
| `page.evaluate()` after 8 s | DataDome's CAPTCHA arrives as a frame from captcha-delivery.com: a page showing it is `challenge`; a block page or an error status with a short page is `blocked`; anything else `content`. Nothing is clicked. |
| `profile` | A returning visitor: Camoufox loads the site once with a new profile, the profile is saved, and a second session with it loads the site again. |
| `LAB_SITES_DATADOME` | The three URLs come from the environment, so the published programs don't name the sites; set your own to repeat the measurement. |

## How it works

1. For each site, take one sticky residential exit and, one session at a time, open each of the twelve setups, load the page once and wait 8 s.
2. Classify what the page shows and record the HTTP status of its first response; a network error gets one retry.
3. Then load the site with Camoufox and a new profile, wait for the profile to be saved, load it again with the same profile and record the second visit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-27):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a music-streaming site | Site B, a business-news publisher | Site C, a business marketplace | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | content (200) | content (200) | 2026-10-27 |
| Chrome, headless | content (200) | challenge (403) | challenge (403) | 2026-10-27 |
| Edge, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| Brave, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| Opera, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| Yandex, headful | content (200) | challenge (403) | challenge (403) | 2026-10-27 |
| Whale, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| Chromium, headless | content (200) | challenge (403) | challenge (403) | 2026-10-27 |
| Patchright, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| Firefox, headful | content (200) | content (200) | challenge (403) | 2026-10-27 |
| WebKit, headful | content (200) | challenge (403) | challenge (403) | 2026-10-27 |
| Camoufox, headful | content (200) | challenge (403) | challenge (403) | 2026-10-27 |
| Camoufox, returning profile (2nd visit) | content (200) | challenge (403) | challenge (403) | 2026-10-27 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headful Chrome was the one setup DataDome let in everywhere.** On 2026-10-27 headful Chrome got the real page on all three sites in all five runs, including site C, where almost every other setup met DataDome's CAPTCHA (`403`) in four of the five runs.
- **Headless gave itself away again.** Headless Chrome and headless Chromium met the CAPTCHA on sites B and C in every run, exactly as on Cloudflare's sites in [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser); on site A (which let everyone in) they got the page.
- **DataDome is where Camoufox struggles.** Camoufox met the CAPTCHA on sites B and C in four of five runs, while on Cloudflare's sites it got everything. On site B, Edge, Brave, Opera, Whale, Patchright and stock Firefox got the page in every run and Camoufox didn't. This matches our sweep, where DataDome had the most sites that stopped Camoufox too.
- **A returning profile changed nothing here.** The second Camoufox visit with a saved profile had the same outcome as the fresh one in all 15 site-and-run cells: DataDome's CAPTCHA came back on the same sites. On Cloudflare a returning profile helped (case 64).
- **One run in five went the other way on site C.** In one run nearly every headful setup got site C, Camoufox and Firefox included, while headless Chrome, Chromium, Brave and Patchright were still challenged: the same site, a different residential exit and a different minute. Treat a single load as a sample, not a verdict.
- **Site A let everyone in** (one network error aside): many sites run DataDome without challenging anyone on their home page. What DataDome shows on its own site is in [case 65](https://cdpfleet.com/docs/cases/datadome-what-gets-through); the overview across vendors is [/bot-protection](https://cdpfleet.com/bot-protection).
