# Does the TLS fingerprint (JA4) explain which browsers got in? Twelve browsers against six vendors

**Medium** · 2026-11-06 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/ja4-vs-vendor-verdicts)

> Each of the twelve browsers from the real-site lab week loads a TLS echo page once, so we know the JA4, peetprint and HTTP/2 fingerprint it sends. Grouped by identical JA4, their recorded outcomes against Cloudflare, DataDome, Akamai, HUMAN, Kasada and AWS WAF (2026-10-26..30) show whether the fingerprint lines up with who got the page. Fingerprints measured 2026-11-06.

## The problem

A common explanation for "this browser gets blocked and that one doesn't" is the TLS fingerprint: the ClientHello a browser sends, summarised as a JA4 string, plus how it opens HTTP/2. Our real-site lab week (2026-10-26 to 10-30) recorded which of twelve browsers got the page behind [Cloudflare](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser), [DataDome](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser), [Akamai](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser), [HUMAN](https://cdpfleet.com/docs/cases/human-real-sites-by-browser), [Kasada and AWS WAF](https://cdpfleet.com/docs/cases/kasada-and-aws-waf-by-browser) on 16 real sites. This case measures the fingerprint each of those twelve setups sends and lines it up with those recorded outcomes: do browsers that send the same JA4 get the same answer, and do different JA4s get different ones? It measures a correlation in one small sample and claims nothing beyond it.

## What we used, and why

| What | Why |
|---|---|
| The twelve setups of the lab week | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, headless Chromium and Patchright (all Chromium-based), Firefox, WebKit and Camoufox (based on Firefox source; not affiliated with Mozilla), launched exactly as in cases 70-77. The returning Camoufox profile of those cases is the same Camoufox build and isn't measured or compared separately. |
| `tls.peet.ws/api/all` | A TLS echo page: it answers with the JA4, the peetprint (a fuller TLS fingerprint) and the Akamai-format HTTP/2 fingerprint of the connection it received. The browser loads it itself with `page.goto()` — Playwright's own `request` client would send a different fingerprint. One load per setup. If it doesn't answer (it has had outages), `tls.browserleaks.com/json` is loaded instead (JA4 and HTTP/2, no peetprint); every published run got its answer from tls.peet.ws. (In one unpublished Java run tls.peet.ws didn't answer Camoufox; tls.browserleaks.com returned the same JA4.) |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit for the whole run, one session at a time, like the lab week. A proxy tunnel passes the browser's own TLS handshake through; our per-version snapshots, measured through a different proxy, got the same fingerprints. |
| The lab week's stored results | Not re-measured: the counts come from the five runs (one per language) stored in cases 70, 71, 73, 75 and 77, counted like the [scoreboard](https://cdpfleet.com/bot-protection/scoreboard): got in = the real page, or an automatic check that cleared by itself; network errors left out. |
| Our per-version snapshots | Every browser build is measured once on tls.peet.ws when it first appears on the fleet. They show which fingerprint each setup's build sent during the lab week, so a browser update between then and today can't be mistaken for a difference. |

## How it works

1. For each of the twelve setups, one after another: launch it through the sticky residential exit, open a new context, load the TLS echo page once and keep its JA4, peetprint and HTTP/2 fingerprint with the browser build.
2. Print every setup's fingerprint.
3. Group the setups by the JA4 they sent in the lab week (today's, checked against the week's snapshots; Yandex by its snapshot, see below).
4. Per vendor and per site, count how often the members of each group got in, and list the pairs with the identical JA4, peetprint and HTTP/2 fingerprint where one got in on every load and the other on none.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-06):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup (JA4 group) | browser.version() today | JA4 today (peetprint · HTTP/2) | Cloudflare, got in (2026-10-26) | DataDome, got in (2026-10-27) | Akamai, got in (2026-10-28) | HUMAN, got in (2026-10-29) | Kasada, got in (2026-10-30) | AWS WAF, got in (2026-10-30) |
|---|---|---|---|---|---|---|---|---|
| J1 · Chrome, headful | 157.0.8103.38 | t13d1517h2_8daaf6152771_cb7bf5808d99 (fc97c1cd · 52d84b11) | 14/14 | 15/15 | 9/14 | 9/14 | 1/10 | 9/9 |
| J1 · Chrome, headless | 157.0.8103.38 | t13d1517h2_8daaf6152771_cb7bf5808d99 (fc97c1cd · 52d84b11) | 5/15 | 4/14 | 5/15 | 5/15 | 0/10 | 5/10 |
| J1 · Opera, headful | 155.0.8059.85 | t13d1517h2_8daaf6152771_cb7bf5808d99 (fc97c1cd · 52d84b11) | 15/15 | 11/15 | 15/15 | 8/15 | 0/10 | 10/10 |
| J1 · Yandex, headful | 26.10.0.0 | t13d1517h2_8daaf6152771_cb7bf5808d99 (fc97c1cd · 52d84b11) | 15/15 (as J2 that week) | 6/15 (as J2 that week) | 5/15 (as J2 that week) | 13/15 (as J2 that week) | 0/10 (build changed that day) | 10/10 (build changed that day) |
| J2 · Edge, headful | 156.0.4314.58 | t13d1516h2_8daaf6152771_806a8c22fdea (8f568b2a · 52d84b11) | 15/15 | 11/15 | 10/15 | 7/14 | 0/10 | 10/10 |
| J2 · Brave, headful | 156.0.8078.80 | t13d1516h2_8daaf6152771_806a8c22fdea (8f568b2a · 52d84b11) | 15/15 | 10/15 | 15/15 | 6/15 | 1/10 | 10/10 |
| J2 · Whale, headful | 150.0.7871.252 | t13d1516h2_8daaf6152771_806a8c22fdea (67c3e911 · 52d84b11) | 15/15 | 11/15 | 10/15 | 14/15 | 0/10 | 10/10 |
| J3 · Chromium, headless | 148.0.7778.96 | t13d1516h2_8daaf6152771_d8a2da3f94cd (1d4ffe9b · 52d84b11) | 5/15 | 5/15 | 5/15 | 5/15 | 0/10 | 5/10 |
| J3 · Patchright, headful | 148.0.7778.96 | t13d1516h2_8daaf6152771_d8a2da3f94cd (1d4ffe9b · 52d84b11) | 10/15 | 10/15 | 10/15 | 6/15 | 0/10 | 10/10 |
| J4 · Firefox, headful | 150.0.2 | t13d1617h2_86a278354501_3cbfd9057e0d (fd4547ee · 6ea73faa) | 10/15 | 11/15 | 10/15 | 5/15 | 0/10 | 9/10 |
| J5 · Camoufox, headful | 156.0.1-beta.34 | t13d1517h2_8daaf6152771_3cbfd9057e0d (a4bbbc7e · 6ea73faa) | 15/15 | 7/15 | 12/15 | 13/15 | 6/10 | 10/10 |
| J6 · WebKit, headful | 26.4 | t13d2913h1_723694b0fccc_5671b5df5029 (e2a4f6ec · HTTP/1.1, no HTTP/2) | 5/15 | 8/15 | 10/15 | 12/15 | 0/10 | 8/10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Twelve setups, six JA4s.** On 2026-11-06, in five runs (one per language) of one load each, Chrome headful and headless, Opera and Yandex sent `t13d1517h2_8daaf6152771_cb7bf5808d99` (J1, Chrome's); Edge, Brave and Whale `t13d1516h2_8daaf6152771_806a8c22fdea` (J2: the same ciphers, one extension fewer, `0xca34`); headless Chromium and Patchright, both build 148, `t13d1516h2_8daaf6152771_d8a2da3f94cd` (J3: without the `0x0904`-`0x0906` signature algorithms); Firefox, Camoufox and WebKit one each. Every Chromium-based setup sent the same HTTP/2 fingerprint; Firefox and Camoufox shared another; WebKit spoke HTTP/1.1 to the echo page. The peetprint split one group: Whale differs from Edge and Brave. Headless and headful Chrome sent identical fingerprints.
- **The lab week's builds sent the same — except Yandex.** For 11 of the 12 setups, today's JA4 and peetprint equal our snapshots of the builds in use on 2026-10-26..30. Yandex changed on 2026-10-30: 26.8 (Chromium 150) sent Whale's exact fingerprint (J2), 26.10 (Chromium 152) sends Chrome's (J1). So below Yandex counts in J2 for Cloudflare, DataDome, Akamai and HUMAN, and is left out for Kasada and AWS WAF, measured the day its build changed.
- **Identical fingerprints, opposite outcomes, on 8 of the 16 sites.** Counting pairs of setups with the identical JA4, peetprint and HTTP/2 fingerprint where one got in on every load and the other on none: 20 pairs, on Cloudflare sites B and C, DataDome B and C, Akamai B and C, HUMAN B and AWS WAF D. Most are headless against headful of the same engine — Chrome headful against Chrome headless on seven of those sites, Patchright 5/5 against headless Chromium 0/5 on three — a difference TLS doesn't carry. Four are between two headful browsers: on Akamai site C (2026-10-28) Opera got in 5/5 and Chrome headful 0/5, Brave 5/5 and Edge 0/5; on Akamai B and DataDome B, Whale 5/5 and Yandex 26.8 0/5.
- **Chrome's JA4 against every other JA4, per vendor** (got in / measured loads, pooled over each vendor's sites and five runs): Cloudflare (2026-10-26) 34/44 vs 139/179; DataDome (10-27) 30/44 vs 109/179; Akamai (10-28) 29/44 vs 116/179; HUMAN (10-29) 22/44 vs 103/178; Kasada (10-30) 1/30 vs 8/110; AWS WAF (10-30) 24/29 vs 96/109. Inside Chrome's own group the range was wider each time: Cloudflare 5/15 (Chrome headless) to 15/15 (Opera), DataDome 4/14 to 15/15, Akamai 5/15 to 15/15, HUMAN 5/15 to 9/14.
- **Within a JA4 group the outcomes spread at least as far as between groups, on five of six vendors.** The widest gap inside one group against the gap between the groups' pooled counts: Cloudflare 5/15 to 15/15 inside J1, groups 5/15 (WebKit) to 15/15 and 60/60; DataDome 4/14 to 15/15 inside J1, groups 7/15 (Camoufox) to 11/15 (Firefox); Akamai 5/15 to 15/15 inside J1 (and Yandex 5/15 to Brave 15/15 inside J2, all headful), groups 15/30 (J3) to 12/15 (Camoufox); HUMAN Brave 6/15 to Whale 14/15 inside J2, groups 5/15 (Firefox) to 13/15 (Camoufox); AWS WAF 5/10 to 10/10 inside J1, groups 15/20 (J3) to 30/30 (J2).
- **Kasada is the one vendor where the groups split — and that group is one browser.** Camoufox's JA4 got in 6/10, every other JA4 at most 1 in 30 (2/100 together: Chrome headful and Brave once each, on site B). Camoufox's JA4 is its own: Chrome's 15-cipher list (`8daaf6152771`) with Firefox's extensions and signature algorithms (`3cbfd9057e0d`); Firefox, one cipher more (`0xc00a`) and the same HTTP/2 fingerprint, got 0/10. With one browser in the group, this sample can't tell its JA4 apart from everything else that differs in Camoufox.
- **What this does and doesn't show.** On these 16 sites in the week of 2026-10-26, browsers with the identical TLS and HTTP/2 fingerprint often got opposite answers, so the fingerprint alone didn't decide the outcome; the clearest split in the data, headless against headful, is invisible in TLS. It doesn't show that vendors ignore TLS — a fingerprint can count in a score without deciding it. The sample is small: 12 setups, five loads per site, one country's exits, one week, fingerprints measured 7-11 days after the outcomes (bridged by the snapshots); no statistic beyond these counts is supported. How each case measured its outcomes: [cases 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser), [71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser), [73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser), [75](https://cdpfleet.com/docs/cases/human-real-sites-by-browser), [77](https://cdpfleet.com/docs/cases/kasada-and-aws-waf-by-browser); the fingerprints of individual browser pairs: [/docs/compare](https://cdpfleet.com/docs/compare).
