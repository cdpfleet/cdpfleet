# CAPTCHA demo pages: what reCAPTCHA v3 scores and hCaptcha show seven browsers, and why a demo can't rank them

**Easy** · 2026-10-24 · Chromium, Google Chrome, Microsoft Edge, Brave, Camoufox, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/captcha-scores-by-browser)

> Google's public reCAPTCHA v3 score demo and hCaptcha's demo widget, loaded once per cell by seven browser setups through your own proxy and a sticky cdpfleet-resi exit. The score each setup got, whether the hCaptcha widget rendered, and why vendors' demo pages tell you little about how a real site will treat your browser.

## The problem

reCAPTCHA v3 gives every visitor a score from 0.0 (likely a bot) to 1.0 (likely a person) without showing anything, and the site decides what to do with it. People often open Google's public v3 demo to "check their score", and hCaptcha's demo to see whether a challenge appears. This case loads both demos once per browser setup and network and records what they report. Nothing is clicked, solved or retried; it is a dated measurement on the vendors' own demo pages, not an evasion guide. For the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).

## What we used, and why

| What | Why |
|---|---|
| Seven setups | Headless Chromium; headful Google Chrome, Microsoft Edge and Brave; Camoufox and Firefox (both based on Firefox source; not affiliated with Mozilla); headful WebKit. Every launch uses `record: false`. |
| `proxy`: your own vs `cdpfleet-resi-country-us-session-<id>` | Your own rotating proxy against one **sticky** residential exit shared by every setup, so a difference between rows on that network is the browser, not the IP. |
| `proxy_updatable` on WebKit | WebKit takes an authenticated HTTP proxy through the updatable route (see [WebKit](https://cdpfleet.com/docs/browsers/webkit)). |
| A fresh context per page | Every load is a first visit, with no Google or hCaptcha cookies from an earlier page. |
| `page.evaluate()` once a second | Google's demo prints its server's verify response, including `"score"`, on the page; the hCaptcha demo is checked for its widget frame. Nothing on either page is touched. |

## How it works

1. For each network (your own proxy, then the sticky Tier 1 exit) and each of the seven setups, launch one session.
2. Load Google's reCAPTCHA v3 demo in a fresh context and wait up to 20 s for the score it prints.
3. Load hCaptcha's demo in another fresh context and wait up to 12 s for its widget to render. Do not click it.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-24):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Network | reCAPTCHA v3 demo score | hCaptcha demo | Measured |
|---|---|---|---|---|
| Chromium, headless | own proxy (rotating) | no score | no widget | 2026-10-24 |
| Google Chrome, headful | own proxy (rotating) | 0.9 | no widget | 2026-10-24 |
| Microsoft Edge, headful | own proxy (rotating) | 0.9 | widget shown | 2026-10-24 |
| Brave, headful | own proxy (rotating) | 0.9 | widget shown | 2026-10-24 |
| Camoufox, defaults | own proxy (rotating) | 0.9 | widget shown | 2026-10-24 |
| Firefox, headful | own proxy (rotating) | 0.9 | widget shown | 2026-10-24 |
| WebKit, headful | own proxy (rotating) | 0.9 | widget shown | 2026-10-24 |
| Chromium, headless | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| Google Chrome, headful | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| Microsoft Edge, headful | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| Brave, headful | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| Camoufox, defaults | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| Firefox, headful | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |
| WebKit, headful | cdpfleet-resi Tier 1, sticky US | 0.9 | widget shown | 2026-10-24 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Every score Google's v3 demo returned was 0.9, for every setup, headless Chromium included.** On 2026-10-24, 60 of 70 cells across five runs got a score and all of them were 0.9: headless Chromium, headful Chrome, Edge and Brave, Camoufox, Firefox and headful WebKit, on your own proxy and on a sticky residential exit alike. The demo page itself says its score "is not a reflection on your Google account or type of traffic", so treat it as a demo, not a verdict.
- **The misses were network, not browser.** 10 cells got no score within 20 s: 9 of them on the rotating own proxy, where the demo's scripts sometimes didn't load in time, and one headless Chromium cell on the residential exit.
- **hCaptcha's demo showed its widget to every setup.** It rendered in 68 of 70 cells (the 2 misses were in one run, on your own proxy); nothing was clicked, so this records only that the widget appears, not whether a challenge would follow.
- **Demo widgets don't separate browsers; real protections do.** The same headless Chromium that scored 0.9 here was blocked by Akamai and challenged by HUMAN ([case 67](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)), stuck on Cloudflare's interstitial ([case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through)) and blocked by DataDome ([case 65](https://cdpfleet.com/docs/cases/datadome-what-gets-through)). Judge a setup by the site you care about, not by a CAPTCHA demo. Turnstile's demo modes are in case 64; which vendor a site uses is case 62 ([which bot protection is this](https://cdpfleet.com/docs/cases/which-bot-protection-is-this)); what pages can read about a browser is [/docs/fingerprinting](https://cdpfleet.com/docs/fingerprinting).
