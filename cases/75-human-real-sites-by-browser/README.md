# HUMAN on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-29 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/human-real-sites-by-browser)

> Three real sites behind HUMAN (formerly PerimeterX), chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, through one sticky residential exit per site, with the HUMAN cookies each load got. Dated 2026-10-29.

## The problem

HUMAN's own site ([case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)) shows its Press & Hold challenge, but every real site sets its own rules. This case takes three real sites behind HUMAN from our sweep of production sites, one that let headless Chrome in (easy), one that stopped headless Chrome but not Camoufox (medium) and one that stopped Camoufox too (hard), and loads each once with every browser the fleet offers, plus a returning Camoufox profile, through one sticky residential exit per site, as [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser), [case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser) and [case 73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser) did for Cloudflare, DataDome and Akamai. It also records which HUMAN cookies (`_px3`, `_pxvid`, `_pxhd`, `pxcts` …) each load got, by name only. The sites aren't named: they stand for a kind of site, and their rules can change any day. It is a dated measurement, not an evasion guide; for the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting), and for the whole picture [/bot-protection](https://cdpfleet.com/bot-protection).

## What we used, and why

| What | Why |
|---|---|
| Twelve browsers | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, Chromium headless and Patchright (all Chromium), Firefox, WebKit, and Camoufox (based on Firefox source; not affiliated with Mozilla). Rebrowser needs a Playwright 1.52 client, so it's left out of a program written for 1.60. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit per site, shared by every setup on that site, so a difference between rows is the browser. |
| One session at a time per site | Each site's setups run one after another; the three sites run side by side. |
| `page.evaluate()` after 8 s | HUMAN's Press & Hold widget (`#px-captcha`), or a short page asking to verify you are human, is `challenge` — usually on a page titled "Access to this page has been denied", but interactive, so not a block; an "access denied" or "has been blocked" page without it, or an error status with a short page, is `blocked`; anything else `content`. Nothing is clicked. |
| `context.cookies()` | The names of the cookies HUMAN set (`_px3`, `_pxvid`, `_pxhd`, `_pxde`, `pxcts` …) 8 s after the load. Values are never read or set. |
| `profile` | A returning visitor: Camoufox loads the site once with a new profile, the profile is saved, and a second session with it loads the site again. |
| `LAB_SITES_HUMAN` | The three URLs come from the environment, so the published programs don't name the sites; set your own to repeat the measurement. |

## How it works

1. For each site, take one sticky residential exit and, one session at a time, open each of the twelve setups, load the page once, wait 8 s, and read the page and HUMAN's cookies.
2. Classify what the page shows and record the HTTP status of its first response; a network error gets one retry.
3. Then load the site with Camoufox and a new profile, wait for the profile to be saved, load it again with the same profile and record the second visit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-29):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a big-box retailer | Site B, a real-estate marketplace | Site C, a freelance marketplace | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | error | challenge (403) | 2026-10-29 |
| Chrome, headless | content (200) | challenge (403) | challenge (403) | 2026-10-29 |
| Edge, headful | content (200) | error | challenge (403) | 2026-10-29 |
| Brave, headful | content (200) | content (200) | challenge (403) | 2026-10-29 |
| Opera, headful | content (200) | content (200) | challenge (403) | 2026-10-29 |
| Yandex, headful | content (200) | content (200) | content (200) | 2026-10-29 |
| Whale, headful | content (200) | content (200) | content (200) | 2026-10-29 |
| Chromium, headless | content (200) | challenge (403) | challenge (403) | 2026-10-29 |
| Patchright, headful | content (200) | content (200) | challenge (403) | 2026-10-29 |
| Firefox, headful | challenge (200) | content (200) | content (200) | 2026-10-29 |
| WebKit, headful | content (200) | content (200) | content (200) | 2026-10-29 |
| Camoufox, headful | content (200) | content (200) | content (200) | 2026-10-29 |
| Camoufox, returning profile (2nd visit) | challenge (200) | content (200) | challenge (403) | 2026-10-29 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **HUMAN never blocked outright; every stop was its Press & Hold challenge.** On 2026-10-29, across five runs of 39 loads each, no load got a block page: a stopped visitor always got the interactive challenge, with HTTP 403 on sites B and C and an HTTP 200 challenge page on site A. The program clicks nothing, so a challenge is the end of the visit.
- **A returning Camoufox profile made things worse.** Fresh Camoufox got the page in 13 of 15 loads (site A 4/5, B 5/5, C 4/5); the second visit with a saved profile was challenged in 14 of 15. That is the reverse of Akamai, where a returning profile turned 2/5 into 5/5 on the hardest site ([case 73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser)), and of DataDome, where it changed nothing ([case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser)): with HUMAN, cookies from an earlier visit counted against the visitor on these sites.
- **Headless was challenged on both protected sites, and let in on the easy one.** Headless Chrome and headless Chromium got the challenge (403) on sites B and C in all five runs, and the page on site A in all five. Site A let every Chromium-based setup in on every run, except Opera once.
- **On the hardest site, headful Chrome was stopped and WebKit got in.** Site C challenged headful Chrome, Brave and Patchright in every run, as it did both headless setups; WebKit got the page in all five runs, Camoufox and Whale in four, Firefox, Opera and Yandex in three, Edge in two. With DataDome, headful Chrome was the one setup let in everywhere ([case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser)).
- **Site B split the Chromium family.** In four of the five runs headful Chrome, Yandex, Whale and fresh Camoufox got the page every time, while Edge, Brave, Opera, Patchright and Firefox were challenged every time and WebKit half the time. In the fifth run (the Node.js one) the site let most setups in, and six of its loads got no HUMAN cookie at all — the same site can apply different rules from one hour to the next.
- **HUMAN set its cookies whether it let you in or not.** `_pxvid` or `_px3` was there after 184 of 193 measured loads, and `_px3` after 86 of 89 challenges — as on HUMAN's own site ([case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)), the cookie is no sign of having passed. Firefox, unlike Camoufox, was challenged on the easy site in four runs of five. The overview across vendors is [/bot-protection](https://cdpfleet.com/bot-protection).
