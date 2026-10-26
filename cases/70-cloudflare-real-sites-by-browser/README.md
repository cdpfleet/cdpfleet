# Cloudflare on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-26 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser)

> Three real sites behind Cloudflare, chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, all through one sticky residential exit per site. The outcome per cell, dated 2026-10-26.

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

The same program in five languages, each verified on the production fleet (last run 2026-10-26):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a fashion retailer | Site B, a design-software site | Site C, a shipping company | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Chrome, headless | content (200) | challenge (403) | challenge (403) | 2026-10-26 |
| Edge, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Brave, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Opera, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Yandex, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Whale, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Chromium, headless | content (200) | challenge (403) | challenge (403) | 2026-10-26 |
| Patchright, headful | content (200) | content (200) | challenge (403) | 2026-10-26 |
| Firefox, headful | content (200) | content (200) | challenge (403) | 2026-10-26 |
| WebKit, headful | content (200) | challenge (403) | challenge (403) | 2026-10-26 |
| Camoufox, headful | content (200) | content (200) | content (200) | 2026-10-26 |
| Camoufox, returning profile (2nd visit) | content (200) | content (200) | content (200) | 2026-10-26 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headless was the tell, not the engine.** On 2026-10-26 headless Chrome and headless Chromium met Cloudflare's challenge (`403`) on sites B and C in all five runs, while headful Chrome, Edge, Brave, Opera, Yandex and Whale, the same Chromium underneath, got the real page on all three sites in every run (one network error aside).
- **Camoufox got all three sites, every time, fresh or returning.** Camoufox headful loaded A, B and C as `content` in all five runs, and so did the returning visit with a saved profile. Site C had stopped Camoufox in our sweep the day before: a real site's verdict moves from day to day, which is why these are dated snapshots.
- **Stealth patches didn't settle it.** Patchright, headful, got site B in 3 of 5 runs and site C in 2 of 5; the plain branded browsers above did better on these sites.
- **WebKit was challenged on B and C, Firefox on C.** Both got site A; WebKit met the challenge on B and C in every run, and stock Firefox on C (it got B). Camoufox, built from Firefox source, got all three.
- **Site A let everyone in.** Every setup, headless ones included, got its page in every run: many sites run Cloudflare without challenging anyone by default.
- **What to try first on a Cloudflare site:** a headful branded browser or Camoufox. For the vendor's own test pages see [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through), for which vendor a site uses [case 62](https://cdpfleet.com/docs/cases/which-bot-protection-is-this), and for the other vendors cases [65](https://cdpfleet.com/docs/cases/datadome-what-gets-through) and [67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through); the overview is [/bot-protection](https://cdpfleet.com/bot-protection).
