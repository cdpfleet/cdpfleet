# Cloudflare: what gets through? Four browsers, three networks, four public test pages

**Medium** · 2026-10-21 · Chromium, Google Chrome, Microsoft Edge, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through)

> Two Cloudflare challenge test pages and a Turnstile test page in two widget modes, loaded once per cell by headless Chromium, headful Chrome, headful Edge and Camoufox through your own proxy and a sticky cdpfleet-resi exit (Tier 1 and Tier 4). Outcome per cell, seconds to clearance, whether `cf_clearance` or a Turnstile token was issued, and what a returning visitor with a profile sees.

## The problem

Cloudflare sits in front of a large share of the web, and "it doesn't load" can mean four different things there: the page loads, an automatic interstitial ("Just a moment…") passes by itself after a few seconds, it stays on an interactive check, or a block page answers. Which one you get depends on the browser (its TLS and HTTP/2 fingerprint, its JavaScript surface), the network the request comes from, and whether you are a returning visitor. This case measures those outcomes on public test pages made for exactly this ([nowsecure.nl](https://nowsecure.nl/), scrapingcourse.com's Cloudflare challenge and peet.ws's Turnstile test), with one load per cell and nothing clicked or solved. It is a dated measurement, not an evasion guide: site owners choose their own Cloudflare settings, and Cloudflare changes its checks all the time. For the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).

## What we used, and why

| What | Why |
|---|---|
| Four setups | Headless Chromium, headful Google Chrome, headful Microsoft Edge (all Chromium) and Camoufox (based on Firefox source; not affiliated with Mozilla), launched with `record: false`. |
| `proxy`: your own vs `cdpfleet-resi-country-us-session-<id>` | Your own proxy (`PROXY_URL`, rotating) against a **sticky** residential exit. Every setup uses the same session name, so they share one exit IP and a difference between rows is the browser, not the network. |
| `cdpfleet-resi-tier-4-…` | The cheaper Tier 4 pool on the two pages where the network could matter, with the two setups that behaved differently on Tier 1. |
| A fresh context per page | Every load is a first visit: no cookie from one page or setup reaches the next. |
| `page.evaluate()` once a second | The title and opening text classify the page (`content`, Cloudflare's interstitial = `challenge`, "Sorry, you have been blocked" = `blocked`); on the Turnstile pages the hidden `cf-turnstile-response` field shows whether a token was issued. Nothing is clicked. |
| `context.cookies()` | `cf_clearance` is the cookie Cloudflare sets when a check passes; only its presence is read. |
| `profile` | A returning visitor: the same profile and the same sticky exit in a second session, to see whether the kept `cf_clearance` skips the check. |

## How it works

1. For each network (your own proxy, then the sticky Tier 1 exit) and each of the four setups, launch one session and load the four test pages once each, in a fresh context.
2. After DOM ready, check the page once a second: a Cloudflare page gets 20 s to clear by itself (seconds to clearance recorded), a Turnstile widget 12 s to issue a token. Then wait up to 6 s for `cf_clearance`.
3. Repeat three of the pages on a sticky Tier 4 exit with headful Chrome and Camoufox.
4. Load scrapingcourse.com's challenge with Camoufox and a new `profile`, end the session, wait for the profile to be saved, and load it again with the same profile and exit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-21):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Network | nowsecure.nl | scrapingcourse.com challenge | Turnstile, non-interactive | Turnstile, managed | Measured |
|---|---|---|---|---|---|---|
| Chromium, headless | own proxy (rotating) | content (cf_clearance) | error | cleared (6 s, token) | challenge | 2026-10-21 |
| Google Chrome, headful | own proxy (rotating) | content (cf_clearance) | challenge | challenge | challenge | 2026-10-21 |
| Microsoft Edge, headful | own proxy (rotating) | challenge (cf_clearance) | challenge | cleared (8 s, token) | challenge | 2026-10-21 |
| Camoufox, defaults | own proxy (rotating) | content (cf_clearance) | cleared (2 s) | cleared (8 s, token) | challenge | 2026-10-21 |
| Chromium, headless | cdpfleet-resi Tier 1, sticky US | content (cf_clearance) | challenge | cleared (7 s, token) | challenge | 2026-10-21 |
| Google Chrome, headful | cdpfleet-resi Tier 1, sticky US | content (cf_clearance) | challenge | cleared (7 s, token) | challenge | 2026-10-21 |
| Microsoft Edge, headful | cdpfleet-resi Tier 1, sticky US | content (cf_clearance) | challenge | cleared (7 s, token) | challenge | 2026-10-21 |
| Camoufox, defaults | cdpfleet-resi Tier 1, sticky US | content (cf_clearance) | cleared (13 s, cf_clearance) | cleared (8 s, token) | challenge | 2026-10-21 |
| Google Chrome, headful | cdpfleet-resi Tier 4, sticky US | content (cf_clearance) | challenge | — | challenge | 2026-10-21 |
| Camoufox, defaults | cdpfleet-resi Tier 4, sticky US | content (cf_clearance) | cleared (18 s, cf_clearance) | — | challenge | 2026-10-21 |
| Camoufox, defaults, returning (profile) | cdpfleet-resi Tier 1, sticky US | — | 1st visit: cleared (11 s, cf_clearance) → 2nd: content (cf_clearance) | — | — | 2026-10-21 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **On scrapingcourse.com's challenge the browser decided, not the network.** On 2026-10-21 headless Chromium, headful Chrome and headful Edge stayed on the interstitial in every cell of every run: your own proxy, the sticky Tier 1 exit and Tier 4 alike. Camoufox got through by itself on at least one network in every run (in 1–18 s), the same page and the same exit. Chrome and Edge headful are real branded builds, so this is not only about headless tells ([case 16](https://cdpfleet.com/docs/cases/headless-tells)): engine-level signals such as the TLS and HTTP/2 fingerprint ([case 01](https://cdpfleet.com/docs/cases/tls-fingerprints)) are part of what Cloudflare scores.
- **nowsecure.nl let every setup in and still ran its check in the background.** The page loaded as `content` for nearly every cell, and `cf_clearance` was issued wherever it loaded: Cloudflare can pass a visitor without showing the interstitial at all. One page passing says little about the next one: site owners set their own security level.
- **Turnstile in managed mode issued no token by itself, for any setup, on any network.** Managed mode decides per visitor whether to show the checkbox; in 12 s none of the 10 setup/network cells got a token without a click, and nothing was clicked. The non-interactive widget on the same site issued a token in most cells, in 4–12 s; the misses (no token within 12 s) were mostly through your own rotating proxy, for every setup at least once.
- **A returning visitor skips the check.** Camoufox with a new `profile` passed scrapingcourse.com's interstitial in 6–18 s and got `cf_clearance`; the next session with the same profile and the same sticky exit loaded the page as `content` straight away, in every run. `cf_clearance` is bound to the visitor (IP and browser), which is why the exit was kept sticky ([case 40](https://cdpfleet.com/docs/cases/persistent-profiles) has profiles in depth).
- **Cloudflare's own Turnstile demo can't tell browsers apart.** Its demo pages use Cloudflare's published test sitekeys, which always pass (or always challenge) by design, so this case uses a test page with a real sitekey instead. These are dated snapshots of four public test pages: which vendor sits in front of a site is in [case 62](https://cdpfleet.com/docs/cases/which-bot-protection-is-this), what fingerprint test pages see per browser in [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see), and the signals themselves in [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).
