# Cloudflare on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-26 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser)

> Three real sites behind Cloudflare, chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, all through one sticky residential exit per site. Measured on 2026-10-26 and again on 2026-11-20, same sites and setup; both dated results are below.

## The problem

Cloudflare's own test pages ([case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through)) show how its checks behave, but a real site sets its own rules. This case takes three real sites behind Cloudflare from our sweep of production sites, one that let headless Chrome in (easy), one that only Camoufox passed (medium) and one that stopped Camoufox the day before (hard), and loads each once with every browser the fleet offers, plus a returning Camoufox profile, through one sticky residential exit per site. The sites aren't named: they stand for a kind of site, and their rules can change any day. It is a dated measurement, not an evasion guide; for the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting), and for the whole picture [/bot-protection](https://cdpfleet.com/bot-protection).

## What we used, and why

| What | Why |
|---|---|
| Twelve browsers | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, Chromium headless and Patchright (all Chromium), Firefox, WebKit, and Camoufox (based on Firefox source; not affiliated with Mozilla). Rebrowser needs a Playwright 1.52 client, so it's left out of a program written for 1.60. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit per site, shared by every setup on that site, so a difference between rows is the browser. |
| One session at a time per site | Each site's setups run one after another; the three sites run side by side. |
| `page.evaluate()` after 8 s | The title and opening text classify the page: Cloudflare's interstitial is `challenge`, a block page or an error status with a short page is `blocked`, anything else `content`. Nothing is clicked. |
| `profile` | A returning visitor: Camoufox loads the site once with a new profile, the profile is saved, and a second session with it loads the site again. |
| `LAB_SITES` | The three URLs come from the environment, so the published programs don't name the sites; set your own to repeat the measurement. |

## How it works

1. For each site, take one sticky residential exit and, one session at a time, open each of the twelve setups, load the page once and wait 8 s.
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

| Setup | Site A, a fashion retailer | Site B, a design-software site | Site C, a shipping company | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Chrome, headless | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Edge, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Brave, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Opera, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Yandex, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Whale, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Chromium, headless | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Patchright, headful | content (200) | content (200) | content (200) | 2026-11-20 |
| Firefox, headful | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| WebKit, headful | content (200) | challenge (403) | challenge (403) | 2026-11-20 |
| Camoufox, headful | content (200) | content (200) | challenge (403) | 2026-11-20 |
| Camoufox, returning profile (2nd visit) | content (200) | content (200) | content (200) | 2026-11-20 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Re-measured on 2026-11-20 (five runs, one per language), same three sites and setup as 2026-10-26.** The table shows the new run; the 2026-10-26 table is kept below it.
- **Headless was still the tell.** Headless Chrome and headless Chromium met Cloudflare's challenge (`403`) on sites B and C in all five runs, exactly as four weeks earlier, while headful Chrome, Opera and Whale, the same Chromium underneath, got the real page on all three sites every time.
- **Site C got stricter for some browsers.** Brave, which got site C in all five runs on 2026-10-26, met the challenge there in 4 of 5 on 2026-11-20; Edge in 1 of 5; a fresh Camoufox in 2 of 5 (it had got C every time). A real site's verdict moves from week to week, which is why these are dated snapshots.
- **A returning profile helped on site C.** Camoufox with a saved profile got all three sites in all five runs, while the fresh Camoufox was challenged on C in 2 of 5: a visitor the site has seen before met fewer checks.
- **Patchright did better this time.** Headful Patchright got sites B and C in all five runs (3 of 5 and 2 of 5 on 2026-10-26).
- **Firefox now met the challenge on site B too.** Stock Firefox was challenged on B in 4 of 5 runs (it got B every time four weeks earlier) and on C in all five, as before; WebKit was challenged on B and C in every run, as before.
- **Site A still let everyone in.** Every setup, headless ones included, got its page in every run on both dates: many sites run Cloudflare without challenging anyone by default.
- **What to try first on a Cloudflare site:** a headful branded browser (Chrome, Opera and Whale held on every site on both dates) or Camoufox with a returning profile, and re-check before relying on it. For the vendor's own test pages see [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through), for which vendor a site uses [case 62](https://cdpfleet.com/docs/cases/which-bot-protection-is-this), and for the other vendors cases [65](https://cdpfleet.com/docs/cases/datadome-what-gets-through) and [67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through); the overview is [/bot-protection](https://cdpfleet.com/bot-protection).
