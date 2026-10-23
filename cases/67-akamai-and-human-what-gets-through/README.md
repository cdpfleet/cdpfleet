# Akamai and HUMAN: what gets through? Four browsers, three networks, the vendors' own sites

**Medium** · 2026-10-23 · Chromium, Google Chrome, Microsoft Edge, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/akamai-and-human-what-gets-through)

> akamai.com (Akamai Bot Manager) and humansecurity.com (HUMAN, formerly PerimeterX), each protected by its own vendor, loaded once per cell by headless Chromium, headful Chrome, headful Edge and Camoufox through your own proxy and sticky cdpfleet-resi exits (Tier 1 and Tier 4). The outcome per cell, the HTTP status, how many sensor posts the vendor's script sent, what Akamai's `_abck` cookie said before and after them, whether HUMAN issued `_px3`, and what a returning visitor with a profile sees.

## The problem

Akamai Bot Manager and HUMAN both decide in two steps: the first response already depends on the request and the IP, and then a script in the page collects signals from the browser and posts them back (a "sensor"), which updates the cookie that later requests carry. A refusal shows as a 403 with an "Access Denied" page (Akamai) or a "Press & Hold" challenge (HUMAN). This case measures, on each vendor's own home page, which setup gets which outcome, and watches the cookies around the sensor posts. It is a dated measurement, not an evasion guide: nothing is clicked, held or solved, every cell is loaded once, and each vendor tunes its own site as it likes. For the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).

## What we used, and why

| What | Why |
|---|---|
| Four setups | Headless Chromium, headful Google Chrome, headful Microsoft Edge (all Chromium) and Camoufox (based on Firefox source; not affiliated with Mozilla), launched with `record: false`. |
| `proxy`: your own vs `cdpfleet-resi-country-us-session-<id>` | Your own proxy (`PROXY_URL`, rotating) against a **sticky** residential exit shared by the four setups. |
| `cdpfleet-resi-tier-4-…` | The cheaper Tier 4 pool, with headful Chrome and Camoufox. |
| A fresh context per page | Every load is a first visit: no cookie from one page or setup reaches the next. |
| `page.on("request")` | Counts the POST requests the vendor's script sends: to akamai.com's own host for Akamai, to `px-cloud.net` / `px-cdn.net` for HUMAN. |
| `context.cookies()` | Akamai's `_abck` carries a status between its first two `~`: `-1` until a sensor post is accepted. Only that field is recorded, at load and after 10 s, never the cookie itself. For HUMAN, whether `_px3` was issued. |
| `page.evaluate()` once a second | Classifies the page: "Access Denied" = `blocked`, HUMAN's Press & Hold (`#px-captcha`) or an Akamai challenge = `challenge`, otherwise the real page. Nothing is clicked. |
| `profile` | A returning visitor: the same profile and the same sticky exit in a second session, keeping the `_abck` the first visit was given. |

## How it works

1. For each network (your own proxy, then a sticky Tier 1 exit) and each of the four setups, launch one session and load both home pages once each, in a fresh context.
2. Record the HTTP status and the cookies, watch the page once a second for 10 s while the vendor's script posts its sensor data, then record the outcome and the cookies again.
3. Load both pages on a sticky Tier 4 exit with headful Chrome and Camoufox.
4. With headful Chrome, load akamai.com with a new `profile`, end the session, wait for the profile to be saved, and load it again with the same profile and exit. Delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-23):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Network | akamai.com (Akamai) | humansecurity.com (HUMAN) | Measured |
|---|---|---|---|---|
| Chromium, headless | own proxy (rotating) | blocked (HTTP 403) | challenge (HTTP 403, _px3, 5 sensor posts) | 2026-10-23 |
| Google Chrome, headful | own proxy (rotating) | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | content (HTTP 200, _px3, 4 sensor posts) | 2026-10-23 |
| Microsoft Edge, headful | own proxy (rotating) | content (HTTP 200, _abck -1 → -1, 2 sensor posts) | content (HTTP 200, _px3, 7 sensor posts) | 2026-10-23 |
| Camoufox, defaults | own proxy (rotating) | content (HTTP 200, _abck -1 → -1, 1 sensor posts) | challenge (HTTP 403, _px3, 3 sensor posts) | 2026-10-23 |
| Chromium, headless | cdpfleet-resi Tier 1, sticky US | blocked (HTTP 403) | challenge (HTTP 403, _px3, 5 sensor posts) | 2026-10-23 |
| Google Chrome, headful | cdpfleet-resi Tier 1, sticky US | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | content (HTTP 200, _px3, 7 sensor posts) | 2026-10-23 |
| Microsoft Edge, headful | cdpfleet-resi Tier 1, sticky US | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | challenge (HTTP 403, _px3, 5 sensor posts) | 2026-10-23 |
| Camoufox, defaults | cdpfleet-resi Tier 1, sticky US | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | challenge (HTTP 403, _px3, 3 sensor posts) | 2026-10-23 |
| Google Chrome, headful | cdpfleet-resi Tier 4, sticky US | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | content (HTTP 200, _px3, 8 sensor posts) | 2026-10-23 |
| Camoufox, defaults | cdpfleet-resi Tier 4, sticky US | content (HTTP 200, _abck -1 → -1, 3 sensor posts) | challenge (HTTP 403, _px3, 3 sensor posts) | 2026-10-23 |
| Google Chrome, headful, returning (profile) | cdpfleet-resi Tier 1, sticky US | 1st visit: content (HTTP 200, _abck -1 → -1, 3 sensor posts) → 2nd: content (HTTP 200, _abck -1 → -1, 3 sensor posts) | — | 2026-10-23 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Akamai turned headless Chromium away at the door; every other setup got the page.** On 2026-10-23, in five runs (one per language), akamai.com answered headless Chromium with 403 "Access Denied" in all 10 of its cells, before any sensor script ran and without setting `_abck`. Headful Chrome, headful Edge and Camoufox got the real page (HTTP 200) in every cell but one network error, on your own rotating proxy, the sticky Tier 1 exit and Tier 4 alike. The first request carries the decision for headless Chromium: its headless tells are visible before any script runs ([case 16](https://cdpfleet.com/docs/cases/headless-tells)).
- **`_abck` stayed unvalidated, and the page was served anyway.** On every load that got akamai.com, the script posted 1–5 sensor payloads within 10 s, yet `_abck`'s status field read `-1` both at load and 10 s later, in all five runs. So on this site the home page doesn't wait for a validated cookie: `-1` means "not validated yet", and what a later request with it would get is up to the site's settings. A returning visitor with a profile kept its `_abck` and got the page again, still at `-1`.
- **HUMAN challenged headless Chromium every time, and usually Camoufox too.** humansecurity.com answered headless Chromium with a 403 Press & Hold challenge in all 10 of its cells. Camoufox was challenged in 12 of its 15 cells, at least once in every run, on every network. Headful Chrome and Edge got the real page on your own proxy in all 10 of their cells there; on the sticky residential exits they got it in 10 of 15, so the same setup was challenged on one exit and let through on another.
- **The two vendors disagreed about Camoufox.** Akamai served it the page in every cell, HUMAN challenged it in most. A setup that works on one vendor's site says little about the next: measure per site, as [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through) does for Cloudflare.
- **HUMAN sets its cookie even when it challenges.** `_px3` was issued on 49 of 50 loads, challenged or not, after 2–14 posts to HUMAN's collector; a cookie alone says nothing about whether the visit passed.
- **These are each vendor's own sites on one day.** Akamai and HUMAN tune their customers' sites per customer, and change their checks often. This case loads only the two vendors' own home pages, once per cell, and clicks nothing. Which vendor protects a site is in [case 62](https://cdpfleet.com/docs/cases/which-bot-protection-is-this), what fingerprint test pages see in [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see), and the signals themselves in [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).
