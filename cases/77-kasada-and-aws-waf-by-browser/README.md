# Kasada and AWS WAF on real sites: twelve browsers, four sites, one exit each

**Medium** · 2026-10-30 · Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Naver Whale, Chromium, Patchright, Firefox, WebKit, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/kasada-and-aws-waf-by-browser)

> Two real sites behind Kasada and two behind AWS WAF, loaded once by each of twelve browsers and by a returning Camoufox profile, through one sticky residential exit per site, with the vendor signals and how long an automatic check took to clear. Dated 2026-10-30.

## The problem

Kasada and AWS WAF were the two vendors our [vendor survey](https://cdpfleet.com/docs/cases/which-bot-protection-is-this) found on too few public pages for their own lab case. This one takes the four real sites from our sweep where each vendor showed up on every load — two Kasada sites (one our sweep rated medium, one hard) and two AWS WAF sites (both easy for headless Chrome in the sweep) — and loads each once with twelve browsers and a returning Camoufox profile, through one residential exit per site, recording what each load ended on and how long an automatic check took.

## What we used, and why

| What | Why |
|---|---|
| Twelve browsers | Chrome headful and headless, Edge, Brave, Opera, Yandex, Whale, Chromium headless and Patchright (all Chromium), Firefox, WebKit, and Camoufox (based on Firefox source; not affiliated with Mozilla). Rebrowser needs a Playwright 1.52 client, so it's left out of a program written for 1.60. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One sticky residential exit per site, shared by every setup on that site, so a difference between rows is the browser. |
| One session at a time per site | Each site's setups run one after another; the four sites run side by side. |
| `page.on("response")` | The status of the page's first response and of the last main-page document 8 s later. Kasada answers a visitor it wants to check with `429` and a script; AWS WAF's automatic check answers `202` with its `challenge.js`. When either clears by itself the page reloads with `200`: that is `cleared`, with the seconds it took. |
| Vendor signals, by name only | Kasada: its `ips.js` script, `x-kpsdk-*` headers and `KP_UIDz` cookies. AWS WAF: an `awswaf.com` or `challenge.js` request, the `x-amzn-waf-action` header and the `aws-waf-token` cookie. Cookie and header values are never read. |
| `page.evaluate()` after 8 s | AWS WAF's CAPTCHA, or a short "verify you are human" page, is `challenge`; a near-empty page (Kasada's `429` answer that never cleared), an "access denied" page or a final error status is `blocked`; anything else `content`. Nothing is clicked. |
| `profile` | A returning visitor: Camoufox loads the site once with a new profile, the profile is saved, and a second session with it loads the site again. |
| `LAB_SITES_KASADA_AWSWAF` | The four URLs come from the environment, so the published programs don't name the sites; set your own to repeat the measurement. |

## How it works

1. For each site, take one sticky residential exit and, one session at a time, open each of the twelve setups, load the page once, wait 8 s, and read the page, the vendor signals and the document statuses.
2. Classify what the page ended on; a network error gets one retry.
3. Then load the site with Camoufox and a new profile, wait for the profile to be saved, load it again with the same profile and record the second visit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A, a footwear retailer (Kasada) | Site B, a property portal (Kasada) | Site C, a travel-booking site (AWS WAF) | Site D, a transit-app site (AWS WAF) | Measured |
|---|---|---|---|---|---|
| Chrome, headful | blocked (429) | cleared (429 → 200) · 7.4 s | cleared (202 → 200) · 3.4 s | error | 2026-10-30 |
| Chrome, headless | blocked (429) | blocked (429) | cleared (202 → 200) · 4.8 s | blocked (202 → 403) | 2026-10-30 |
| Edge, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 3.9 s | cleared (202 → 200) · 4.6 s | 2026-10-30 |
| Brave, headful | blocked (429) | cleared (429 → 200) · 7.8 s | cleared (202 → 200) · 3.1 s | cleared (202 → 200) · 4.9 s | 2026-10-30 |
| Opera, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 3.4 s | cleared (202 → 200) · 4.6 s | 2026-10-30 |
| Yandex, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 5.2 s | cleared (202 → 200) · 4.6 s | 2026-10-30 |
| Whale, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 5.1 s | cleared (202 → 200) · 4.7 s | 2026-10-30 |
| Chromium, headless | blocked (429) | blocked (429) | cleared (202 → 200) · 4.3 s | blocked (202 → 403) | 2026-10-30 |
| Patchright, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 5.1 s | cleared (202 → 200) · 4.6 s | 2026-10-30 |
| Firefox, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 4.4 s | cleared (202 → 200) · 4.8 s | 2026-10-30 |
| WebKit, headful | blocked (429) | blocked (429) | cleared (202 → 200) · 5.8 s | cleared (202 → 200) · 4.6 s | 2026-10-30 |
| Camoufox, headful | cleared (429 → 200) · 3.9 s | blocked (429) | cleared (202 → 200) · 3.9 s | cleared (202 → 200) · 4.5 s | 2026-10-30 |
| Camoufox, returning profile (2nd visit) | content (200) | content (200) | error | content (200) | 2026-10-30 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Kasada turned away every browser but Camoufox.** On 2026-10-30, across five runs (one per language), the eleven setups other than Camoufox got Kasada's `429` and a page that stayed blank in 108 of their 110 loads on the two Kasada sites; headful Chrome and Brave each got through once on site B, after 7.4 and 7.8 s. Kasada shows no CAPTCHA: its `429` page runs a script, and the visit either reloads with `200` by itself or stays blank. That is stricter than Akamai's hardest site, where Brave and Opera got in every run ([case 73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser)).
- **Camoufox got through on the easier Kasada site, every time.** On site A fresh Camoufox got the same `429` and cleared it by itself in all five runs, the page reloading with `200` 3.9–5.2 s after the load started; on site B (the one our sweep rated hard) it cleared once in five. A returning Camoufox profile went straight to the page (`200`, no check) on site A in all five runs, and on site B in the two runs where its second session launched (three network errors).
- **AWS WAF checked everyone and let nearly everyone through.** Both AWS WAF sites answered every first load with `202` and its `challenge.js`; the check then passed by itself and the page reloaded with `200`, in a median 4.4 s on site C and 5.2 s on site D (3.0–20.3 s, counted from the start of the load). Site C let every setup through in 59 of 60 loads, headless included; on site D the ten headful setups cleared in 47 of 49 measured loads. No load on either site ended on a CAPTCHA.
- **Site D refused headless, site C didn't.** Headless Chrome and headless Chromium got `202` and then `403` on site D in all five runs, while on site C they cleared like everyone else. Site D's first answer carried `x-amzn-waf-action: challenge` on 59 loads; site C sent no such header and relied on `challenge.js` alone — two sites on the same vendor, set up differently.
- **A saved profile skips the check on both vendors.** With a profile from its first visit, Camoufox's second session got the page with `200` and no interstitial on both AWS WAF sites in every run where it launched (9 of 9) and on Kasada site A in five of five. Kasada set `KP_UIDz` cookies on site B on every load, let in or not; site A set no Kasada cookie at all — its signal was the `ips.js` script.
- These are four sites on one day through one country's exits; vendors and site owners change their settings often. How the same browsers fared against Cloudflare, DataDome, Akamai and HUMAN: [cases 70](https://cdpfleet.com/docs/cases/cloudflare-real-sites-by-browser), [71](https://cdpfleet.com/docs/cases/datadome-real-sites-by-browser), [73](https://cdpfleet.com/docs/cases/akamai-real-sites-by-browser) and [75](https://cdpfleet.com/docs/cases/human-real-sites-by-browser), and the overview on [/bot-protection](https://cdpfleet.com/bot-protection).
