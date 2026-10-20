# Which bot protection is this? Detect the vendor from cookies, headers and requests

**Easy** · 2026-10-20 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/which-bot-protection-is-this)

> Load 20 public pages once each (the vendors' own demo pages plus large public sites) and name the bot-protection vendor from the signals it leaves: response headers, cookie names and the hosts the page calls. Counts and signals per vendor, with the outcome each page showed on the measurement date.

## The problem

Before you tune a browser, a proxy or a wait for a page that won't load, find out what is in front of it. "Blocked" means different things behind Cloudflare, DataDome, Akamai, HUMAN or Kasada, and each one is recognisable from the outside: it sets cookies with known names, adds response headers, and loads its scripts from its own hosts. A detector that reads those signals from one ordinary page load tells you which vendor you are dealing with, and which of the later lab cases applies. This case only observes: it loads public pages once, solves nothing and retries nothing.

## What we used, and why

| What | Why |
|---|---|
| `response.allHeaders()` on the main document | Vendor headers: `cf-ray` (Cloudflare), `x-datadome` (DataDome), `akamai-*` (Akamai), `x-kpsdk-*` (Kasada), `x-iinfo` (Imperva). |
| `context.cookies()` after 6 s | Cookie names the protection scripts set: `__cf_bm`/`cf_clearance`, `datadome`, `_abck`/`bm_sz`, `_px*`, `incap_ses_*`/`visid_incap_*`, `aws-waf-token`. Only the names are read. |
| `request` events | Every URL the page loads: `/cdn-cgi/challenge-platform`, the Turnstile widget on `challenges.cloudflare.com`, `captcha-delivery.com`, HUMAN's `px-cdn.net`/`px-cloud.net`, Kasada's `ips.js`, `awswaf.com`, reCAPTCHA and hCaptcha. |
| Title and the first lines of text | The outcome in the lab's words: `content`, `challenge` (an interactive CAPTCHA or "press and hold"), `blocked` (a block page or a short 403/429), or `error`. |
| A fresh context per page | No cookies carried from one site to the next, so each page is a first visit. |
| One baseline setup | Headless Chromium on cdpfleet through your own proxy (`PROXY_URL`). The cases later this week compare browsers and networks on the same vendors. |

## How it works

1. For each of 20 public pages, home or demo pages only: the vendors' own test pages (nowsecure.nl, scrapingcourse.com's Cloudflare challenge, peet.ws's Turnstile test, Google's reCAPTCHA v3 demo, hCaptcha's demo) and two or three large public sites per vendor, open a fresh context and load it once.
2. Record every request URL; after DOM ready read the main response's headers, wait 6 s for the protection scripts, then read the title, the opening text and the cookie names.
3. Match the signals for each vendor, classify the outcome, and add the page to every vendor found. Print counts per vendor and signal, not a list of sites; only the demo pages are named.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-20):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Vendor | Pages (of 20) | Signals seen (pages) | Outcomes | Measured |
|---|---|---|---|---|
| Cloudflare | 7 | cf-ray header 6, __cf_bm / cf_clearance cookie 5, /cdn-cgi/challenge-platform request 6 | content 5, challenge 2 | 2026-10-20 |
| Cloudflare Turnstile | 4 | Turnstile widget 4 | content 2, challenge 2 | 2026-10-20 |
| DataDome | 3 | x-datadome header 2, datadome cookie 3, DataDome host (captcha-delivery.com, datadome.co) 2 | content 1, challenge 2 | 2026-10-20 |
| Akamai | 3 | akamai header 3, _abck / bm_sz cookie 1 | content 1, blocked 2 | 2026-10-20 |
| HUMAN (PerimeterX) | 2 | HUMAN host (px-cdn.net, px-cloud.net) 2, _px cookie 1 | content 1, challenge 1 | 2026-10-20 |
| Kasada | 2 | x-kpsdk header 1, ips.js / kpsdk request 2 | blocked 2 | 2026-10-20 |
| AWS WAF | 2 | aws-waf-token cookie 2, AWS WAF host (awswaf.com) 1 | content 2 | 2026-10-20 |
| Google reCAPTCHA | 1 | reCAPTCHA script 1 | content 1 | 2026-10-20 |
| hCaptcha | 1 | hCaptcha host 1 | content 1 | 2026-10-20 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One page load is enough to name the vendor.** On 2026-10-20 the detector found the vendor seen the day before on 19–20 of the 20 pages in each of the five runs, and at most one page per run showed no signal. Imperva didn't show up on any of these pages.
- **Most pages carry more than one vendor.** All the Turnstile, Cloudflare-challenge and hCaptcha demo pages are themselves served through Cloudflare, so Cloudflare was found on 7–9 pages while the `cf-ray` header was on 6 in every run. Count the vendor that decides the outcome, not every vendor present.
- **A header shows the CDN; a cookie or script shows the bot product.** The `akamai-*` header was on all 3 Akamai pages in every run, but the Bot Manager cookie (`_abck`/`bm_sz`) on only 1–3 of them. The same goes for `cf-ray` (Cloudflare's CDN) versus `__cf_bm` and `/cdn-cgi/challenge-platform` (its bot management). The cookie set by scripts after load is the stronger signal, which is why the detector waits 6 s.
- **What the baseline saw (headless Chromium through your own proxy, 2026-10-20):** DataDome showed its CAPTCHA (a `captcha-delivery.com` frame) on 2 of its 3 pages in every run; Kasada returned a block on every page where it was detected (1–2); Akamai blocked 1–2 of 3; HUMAN showed a challenge on 1 of its 2–3; both AWS WAF pages loaded as content. These are dated snapshots of one setup on one day; the cases later this week compare browsers and networks on the same vendors.
- **The vendors' own demo pages were stable across all five runs:** nowsecure.nl, the peet.ws Turnstile test, Google's reCAPTCHA v3 demo and the hCaptcha demo loaded as `content` with their widget present, and scrapingcourse.com's Cloudflare challenge stayed on its interactive challenge. Nothing was solved or retried; a widget being present is not a pass.
