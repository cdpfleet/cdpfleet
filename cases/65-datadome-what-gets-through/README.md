# DataDome: what gets through? Four browsers, three networks, DataDome's own site

**Medium** · 2026-10-22 · Chromium, Google Chrome, Microsoft Edge, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/datadome-what-gets-through)

> DataDome's own home and pricing pages, which DataDome protects, loaded once per cell by headless Chromium, headful Chrome, headful Edge and Camoufox through your own proxy and sticky cdpfleet-resi exits (Tier 1 and Tier 4). Outcome per cell with the HTTP status DataDome answered, what a returning visitor with a profile sees, and whether Camoufox's `humanize` changes the next page.

## The problem

DataDome decides on the very first request: an intercepted page comes back as a 403 that embeds a frame from captcha-delivery.com, and that frame is either a device check that passes by itself, a CAPTCHA, or a block. Which one you get depends on the browser, the IP it comes from and that IP's recent history, and on what the visitor did before. This case measures the outcomes on DataDome's own public site (the vendor protects its own pages, so the protection is real and the pages are public), with one load per cell and nothing clicked or solved. It is a dated measurement, not an evasion guide: site owners choose their own DataDome settings, and DataDome changes its models all the time. For the signals a page can read, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting).

## What we used, and why

| What | Why |
|---|---|
| Four setups | Headless Chromium, headful Google Chrome, headful Microsoft Edge (all Chromium) and Camoufox (based on Firefox source; not affiliated with Mozilla), launched with `record: false`. |
| `proxy`: your own vs `cdpfleet-resi-country-us-session-<id>` | Your own proxy (`PROXY_URL`, rotating) against **sticky** residential exits. DataDome scores the IP and its history, so every setup gets its own sticky exit: one setup's visits can't change what the next one sees. |
| `cdpfleet-resi-tier-4-…` | The cheaper Tier 4 pool on the home page, with headful Chrome and Camoufox. |
| A fresh context per page | Every load is a first visit: no cookie from one page or setup reaches the next. |
| The navigation status and `page.evaluate()` once a second | A 403 means DataDome intercepted the request. The iframe it embeds tells the rest: `/captcha/` is a CAPTCHA (`challenge`), `t=bv` a block (`blocked`), anything else a device check; the real page after a check is `cleared`, the real page straight away `content`. Nothing is clicked. |
| `profile` | A returning visitor: the same profile and the same sticky exit in a second session, keeping the `datadome` cookie the first visit was given. |
| `humanize` | Camoufox moves the cursor along human-like paths when it's on (the default) and jumps when it's off. Three moves on the home page, then the pricing page in the same tab, so DataDome's script can see the movement before the next request. |

## How it works

1. For each network (your own proxy, then sticky Tier 1 exits) and each of the four setups, launch one session and load the home and pricing pages once each, in a fresh context.
2. Record the HTTP status, then check the page once a second for up to 12 s: a CAPTCHA or block ends the watch, a device check gets the full 12 s to pass by itself.
3. Load the home page on sticky Tier 4 exits with headful Chrome and Camoufox.
4. With headful Chrome and Camoufox, load the home page with a new `profile`, end the session, wait for the profile to be saved, and load it again with the same profile and exit. Delete the profiles.
5. With Camoufox, `humanize` on and then off (each with its own exit): load the home page, move the cursor three times, load the pricing page in the same tab.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-22):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Network | datadome.co | datadome.co/pricing | Measured |
|---|---|---|---|---|
| Chromium, headless | own proxy (rotating) | blocked (HTTP 403) | blocked (HTTP 403) | 2026-10-22 |
| Google Chrome, headful | own proxy (rotating) | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |
| Microsoft Edge, headful | own proxy (rotating) | content (HTTP 200) | challenge (HTTP 403) | 2026-10-22 |
| Camoufox, defaults | own proxy (rotating) | challenge (HTTP 403) | cleared (HTTP 403, 22 s) | 2026-10-22 |
| Chromium, headless | cdpfleet-resi Tier 1, sticky US | blocked (HTTP 403) | blocked (HTTP 403) | 2026-10-22 |
| Google Chrome, headful | cdpfleet-resi Tier 1, sticky US | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |
| Microsoft Edge, headful | cdpfleet-resi Tier 1, sticky US | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |
| Camoufox, defaults | cdpfleet-resi Tier 1, sticky US | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |
| Google Chrome, headful | cdpfleet-resi Tier 4, sticky US | content (HTTP 200) | — | 2026-10-22 |
| Camoufox, defaults | cdpfleet-resi Tier 4, sticky US | content (HTTP 200) | — | 2026-10-22 |
| Google Chrome, headful, returning (profile) | cdpfleet-resi Tier 1, sticky US | 1st visit: challenge (HTTP 403), cookie kept → 2nd: content (HTTP 200) | — | 2026-10-22 |
| Camoufox, defaults, returning (profile) | cdpfleet-resi Tier 1, sticky US | 1st visit: content (HTTP 200), cookie kept → 2nd: content (HTTP 200) | — | 2026-10-22 |
| Camoufox, humanize on (cursor moved 1.8 s) | cdpfleet-resi Tier 1, sticky US | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |
| Camoufox, humanize off (cursor moved 0 s) | cdpfleet-resi Tier 1, sticky US | content (HTTP 200) | content (HTTP 200) | 2026-10-22 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headless Chromium got DataDome's block page, almost everywhere.** On 2026-10-22, across five runs, it was answered with the block page (HTTP 403, `t=bv`) in 19 of its 20 cells, through your own proxy and the sticky residential exit alike, and got the real page once. A headless build carries tells a page can read ([case 16](https://cdpfleet.com/docs/cases/headless-tells)), and DataDome scores them before the page is served.
- **Headful Chrome, Edge and Camoufox mostly got through, and nothing guaranteed it.** On the same two pages they loaded the real page in most cells, but each of them hit DataDome's CAPTCHA in some runs. Edge happened to get the page in all 10 of its cells on the Tier 1 exits; Chrome and Camoufox had whole runs on the CAPTCHA there. The network made no consistent difference: CAPTCHAs came up through your own rotating proxy, the Tier 1 exits and Tier 4 alike.
- **DataDome decides on the first response.** Every CAPTCHA and every block came back as HTTP 403 on the page request itself, and every real page as 200. Its device check let a browser through by itself only 3 times in five runs (in 21–33 s from the start of the load); everything else was settled on that first response.
- **A returning visitor did no worse.** The first visit set the `datadome` cookie in every run, and the profile kept it. Chrome's first visit hit the CAPTCHA in 3 of 5 runs, and the second session with the same profile and exit loaded the real page in all three; Camoufox's second visit was as good as or better than its first in every run (one run stayed on the CAPTCHA both times). The cookie is how DataDome recognises a visitor it has already scored, which is why the exit stays sticky ([case 40](https://cdpfleet.com/docs/cases/persistent-profiles) has profiles in depth).
- **`humanize` made no measurable difference here.** The page requested after three cursor moves loaded in every run with `humanize` on, and in 4 of 5 with it off; the one miss had already hit the CAPTCHA on the home page, before the cursor moved. Which vendor sits in front of a site is in [case 62](https://cdpfleet.com/docs/cases/which-bot-protection-is-this), what fingerprint test pages see in [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see), Cloudflare's side in [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through), and the signals themselves in [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting). These are dated snapshots of one vendor's own site; other sites run DataDome with their own settings.
