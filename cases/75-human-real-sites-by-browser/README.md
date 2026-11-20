# HUMAN on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-29 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/human-real-sites-by-browser)

> Three real sites behind HUMAN (formerly PerimeterX), chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, through one sticky residential exit per site, with the HUMAN cookies each load got. Measured on 2026-10-29 and again on 2026-11-20, same sites and setup; both dated results are below.

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

The same program in five languages, each verified on the production fleet (last run 2026-11-20):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a big-box retailer | Site B, a real-estate marketplace | Site C, a freelance marketplace | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Chrome, headless | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Edge, headful | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Brave, headful | challenge (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Opera, headful | challenge (200) | challenge (403) | content (200) | 2026-11-20 |
| Yandex, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Whale, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Chromium, headless | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Patchright, headful | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Firefox, headful | content (200) | challenge (403) | content (200) | 2026-11-20 |
| WebKit, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Camoufox, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Camoufox, returning profile (2nd visit) | challenge (200) | challenge (403) | challenge (403) | 2026-11-20 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Re-measured on 2026-11-20 (five runs, one per language), same three sites and setup as 2026-10-29.** The table shows the new run; the 2026-10-29 table is kept below it.
- **HUMAN still never blocked outright.** Across the five runs (194 measured loads) no load got a block page: every stop was the Press & Hold challenge, with HTTP 403 on sites B and C and an HTTP 200 challenge page on site A. The program clicks nothing, so a challenge is the end of the visit.
- **A returning Camoufox profile was challenged every time, and fresh Camoufox did worse too.** The second visit with a saved profile was challenged on all 15 loads (14 of 15 on 2026-10-29). Fresh Camoufox got the page in 8 of 15 (site A 2/5, B 5/5, C 1/5), down from 13 of 15. Cookies from an earlier visit still counted against the visitor on these sites, unlike on Akamai's ([case 73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser)).
- **Headless was challenged on both protected sites, and let in on the easy one,** in every run on both dates.
- **On the hardest site, WebKit and Opera got in every time.** Site C let WebKit in on all five runs on both dates, and Opera now too (3 of 5 before), Whale in four. Headful Chrome, Brave and Patchright were challenged in every run as before, and Edge now as well (it got in twice four weeks earlier).
- **Site B's split held in every run this time.** Headful Chrome, Yandex, Whale and fresh Camoufox got the page in all five runs, WebKit in three, and Edge, Brave, Opera, Patchright and Firefox were challenged in all five. On 2026-10-29 one run had let most setups in.
- **Site A got a little stricter for Chromium browsers and easier for Firefox.** Brave and Opera were challenged there in 2 of 5 runs, headful Chrome and Whale in 1; stock Firefox got in on 3 of 5 (1 of 5 before).
- **HUMAN set its cookies whether it let you in or not.** `_pxvid` or `_px3` was there after 185 of 194 measured loads, and `_px3` after 98 of 105 challenges, so the cookie is no sign of having passed (as on HUMAN's own site, [case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)). The overview across vendors is [/bot-protection](https://cdpfleet.com/bot-protection).
