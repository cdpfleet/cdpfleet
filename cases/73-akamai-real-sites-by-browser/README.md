# Akamai on real sites: twelve browsers, three sites, one exit each

**Medium** · 2026-10-28 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser)

> Three real sites behind Akamai Bot Manager, chosen from our sweep as easy, medium and hard, loaded once by each of twelve browsers and by a returning Camoufox profile, through one sticky residential exit per site, with the state of Akamai's _abck cookie. Dated 2026-10-28.

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

The same program in five languages, each verified on the production fleet (last run 2026-10-28):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a bank | Site B, a home-improvement retailer | Site C, a game-key marketplace | Measured |
|---|---|---|---|---|
| Chrome, headful | content (200) | error | blocked (403) · _abck -1 | 2026-10-28 |
| Chrome, headless | content (200) | blocked (200) | blocked | 2026-10-28 |
| Edge, headful | content (200) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-10-28 |
| Brave, headful | content (200) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-10-28 |
| Opera, headful | content (200) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-10-28 |
| Yandex, headful | content (200) | blocked (200) | blocked (200) · _abck -1 | 2026-10-28 |
| Whale, headful | content (200) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-10-28 |
| Chromium, headless | content (200) | blocked (200) | blocked | 2026-10-28 |
| Patchright, headful | content (200) | content (200) · _abck -1 | blocked (403) · _abck -1 | 2026-10-28 |
| Firefox, headful | content (200) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-10-28 |
| WebKit, headful | content (200) | content (200) · _abck -1 | blocked (200) · _abck -1 | 2026-10-28 |
| Camoufox, headful | content (200) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-10-28 |
| Camoufox, returning profile (2nd visit) | content (200) | content (200) · _abck -1 | content (200) · _abck -1 | 2026-10-28 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headless was refused on both protected sites, before Akamai even set its cookie.** On 2026-10-28 headless Chrome and headless Chromium got Akamai's block page on site B in all five runs, with no `_abck` cookie at all, and on site C Akamai reset the connection (`ERR_HTTP2_PROTOCOL_ERROR`) every time. Yandex, though headful, was blocked on site B the same way in every run.
- **On the hardest site the winners were Brave and Opera, not Chrome.** Site C let Brave and Opera in on all five runs and blocked headful Chrome, Edge, Whale, Yandex and Patchright in every run, the reverse of DataDome's sites, where headful Chrome was the one setup let in everywhere ([case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser)). Firefox and WebKit were blocked on site C too.
- **A returning Camoufox profile helped on Akamai.** On site C a fresh Camoufox got the page in two of five runs; the second visit with a saved profile got it in all five. On DataDome's sites a returning profile changed nothing; on Cloudflare's it helped, as here.
- **`_abck` never validated, and it didn't decide the page.** Wherever Akamai set `_abck`, its status read `-1` right after the page loaded and still `-1` 8 s later, in every cell of every run: on site B the page was served with it unvalidated, on site C most setups were blocked with the same `-1`. Akamai's own site behaved the same way ([case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)).
- **Site B let every other headful browser in.** Headful Chrome, Edge, Brave, Opera, Whale, Patchright, Firefox, WebKit and Camoufox (fresh and returning) got site B in every run (one network error aside).
- **Site A let everyone in** and set no `_abck` cookie on its home page: a site can use Akamai for some pages or actions without checking its front door. The overview across vendors is [/bot-protection](https://cdpfleet.com/bot-protection); Cloudflare's and DataDome's real sites are [case 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser) and [case 71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser).
