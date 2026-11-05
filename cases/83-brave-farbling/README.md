# Brave's fingerprint randomisation per site, per context and per launch, and what public fingerprint pages make of it

**Medium** · 2026-11-05 · Brave, Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/brave-farbling)

> Brave adds noise to canvas, audio, cores, memory and the plugin list, keyed to the site and the session. One probe on two made-up sites, the same site again, a second context and a second launch shows exactly when the values change, next to headful Chrome. Then CreepJS, browserleaks, sannysoft, deviceandbrowserinfo and the fingerprint.com playground give their own verdicts on Brave headless, Brave headful and Chrome. Dated 2026-11-05.

## The problem

Brave doesn't hide its fingerprint, it randomises it ("farbling"): canvas and audio output get a little noise, `hardwareConcurrency` and `deviceMemory` are made up, and the plugin list gets invented names. [Case 66](https://cdpfleet.com/docs/cases/fingerprint-stability-across-launches) found Brave's canvas and audio new on every launch, on one site. But the noise is said to be keyed to the site as well, so the useful questions are finer: do two sites in the same session see different values, does the same site see the same ones when you come back, what does a second browser context get, and what do public fingerprint pages conclude when they meet a browser that randomises? This case measures that on a dated day, on public test pages and on two sites served by the program itself.

## What we used, and why

| What | Why |
|---|---|
| Three setups | `brave` with `headless: true`, `brave` with `headless: false`, and `chrome` with `headless: false` as the reference. Brave runs with its default Shields; nothing is configured. Every launch uses `record: false` and the same proxy. |
| Two made-up sites via `context.route()` | `https://site-a.example/` and `https://site-b.test/` are two different registrable domains, so the browser treats them as two first-party sites; `route.fulfill()` serves a one-line page for both, and no request leaves the browser. |
| A small fingerprinting probe | The same script in every language reads a canvas hash (drawn twice, to check the page agrees with itself), an `OfflineAudioContext` hash, `navigator.hardwareConcurrency`, `navigator.deviceMemory`, a hash of the `navigator.plugins` names, `screen.width × height` and `outerWidth × outerHeight`. |
| Six visits per setup, each in a new tab | Launch 1: site A, site B, site A again, then site A in a second `browser.newContext()`. Launch 2 (a new session): site A and site B. |
| Five public test pages, one load each per setup | CreepJS (its headless and stealth scores, the "privacy" it names, its canvas noise estimate and the sections it marks as lies), browserleaks.com/canvas (signature and uniqueness), bot.sannysoft.com, deviceandbrowserinfo.com and the fingerprint.com playground (bot verdict and suspect score only), read the way [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see) reads them. |

## How it works

1. For each of the three setups, launch a session and add routes for the two made-up sites to a new context.
2. Visit site A, site B and site A again, each in a new tab, running the probe; then site A in a second context of the same session.
3. Close the session, launch a new one, and visit site A and site B again.
4. In a fresh context of that second session, load each public test page once, wait for its verdict and read it from the page.
5. Print the probe values of all six visits and the five verdicts per setup.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-05):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Site A vs site B (one session) | Site A again (new tab) | Site A, second context | Site A, next launch | Cores · memory · screen (6 visits) | CreepJS: privacy · lies · headless | browserleaks canvas | sannysoft · deviceandbrowserinfo · fingerprint.com |
|---|---|---|---|---|---|---|---|---|
| Brave, headless | canvas, audio, cores, memory, plugins, window size differ | all the same | canvas, audio, cores, memory, plugins, window size differ | canvas, audio, cores, plugins, window size differ | 39, 45, 17, 14, 7 · 16, 8, 4 GB · 1366x768 | Brave · Screen, Canvas 2d, Audio, Navigator · 67% | 100% (The signature is unique to our database) | 4 of 31 failed (User Agent (Old), WebGL Renderer, HEADCHR_UA, CHR_MEMORY) · bot · You are a bad bot (type: headless_chrome), score 41 |
| Brave, headful | canvas, audio, cores, memory, plugins, window size differ | all the same | canvas, audio, cores, plugins, window size differ | canvas, audio, cores, plugins, window size differ | 10, 62, 50, 33, 29 · 16, 8, 4 GB · 1366x768 | Brave · Screen, Canvas 2d, Audio, Navigator · 0% | 100% (The signature is unique to our database) | 1 of 31 failed (WebGL Renderer) · bot · Not detected, score 33 |
| Google Chrome, headful | all the same | all the same | all the same | cores differ | 36, 64 · 32 GB · 1280x720 | unknown · none · 0% | 99.95% (119 of 258037 user agents have the same signature) | 1 of 31 failed (WebGL Renderer) · bot · You are a bad bot (type: chromium_automation), score 47 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **On 2026-11-05, Brave showed two different sites in the same session two different devices.** Across five runs (one per language) and both Brave setups, site A and site B got a different canvas hash, audio hash, plugin list and window size in 20 of 20 same-session pairs, different `hardwareConcurrency` in 19 and different `deviceMemory` in 15. Headful Chrome gave both sites exactly the same values every time. So these values can't tie one Brave session's visit to one site to its visit to another.
- **The same site got the same values back within a session.** Site A in a new tab of the same context matched the first visit on all seven signals in all 10 Brave sessions: a site sees one stable browser for as long as the session lasts.
- **A second browser context in the same session counted as a new session.** Site A in a second `browser.newContext()` got a new canvas, audio, plugin list, core count and window size in 10 of 10 sessions (memory in 8 — it only has four values to pick from). With automation every new context is a new Brave seed; to keep one site's values, keep its context.
- **The next launch changed everything for both sites again**, as [case 66](https://cdpfleet.com/docs/cases/fingerprint-stability-across-launches) found for one site, which also covers profiles, WebGL and fonts.
- **Cores, memory and plugins were made up.** Brave reported `hardwareConcurrency` from 4 to 63 and `deviceMemory` of 4, 8, 16 or 32 across its 60 visits. Chrome reported the server's real core count (16, 24, 36 or 64, the same within every session — it changed between launches only when the session landed on a different server) and 32 GB every time. Brave's plugin list always had 7 entries: the four PDF viewer names it shares with Chrome (`PDF Viewer`, `Chromium PDF Viewer`, `Microsoft Edge PDF Viewer`, `WebKit built-in PDF`), two random strings and one made-up PDF plug-in name such as `Online com.adobe.pdf extension` (once it came out as a second `Chromium PDF Viewer`), in shuffled order; Chrome listed the standard five.
- **Brave didn't report Playwright's viewport as its screen.** With Playwright's default 1280×720 viewport, Chrome reported a 1280×720 screen and a 1288×805 window on all 30 visits. Brave reported a 1366×768 screen on 55 of 60 visits and 1280×800 on 5 (in both modes), and its `outerWidth × outerHeight` was the viewport plus 0–8 px in each direction, picked per site.
- **CreepJS recognised Brave and marked its noise as lies.** In all 10 Brave loads it named the privacy feature "Brave", estimated the canvas noise at 11–13% of rgba values and marked the Screen, Canvas 2d, Audio and Navigator sections as lies; for Chrome it named no privacy feature and marked nothing, 5 of 5. Its headless score was 67% for Brave headless (which still sends a `HeadlessChrome` user agent) and 0% for Brave headful and Chrome; "like headless" was 50% and stealth 0% for all three.
- **browserleaks called Brave's canvas unique, Chrome's common.** All 10 Brave canvas signatures were different and each was "unique to our database"; Chrome got the same signature in all 5 runs, shared by 119 of 258,037 user agents in browserleaks' database. Noise makes every visit unique — which also means no other visitor looks like it.
- **The bot-test pages judged automation, not farbling.** sannysoft failed Brave headless on 4 checks (`User Agent (Old)`, `HEADCHR_UA`, `CHR_MEMORY`, WebGL renderer) and Brave headful and Chrome on the WebGL renderer only; deviceandbrowserinfo called all three a bot in every run for CDP automation (plus a bot user agent for Brave headless). The fingerprint.com playground called Brave headless a `headless_chrome` bad bot and Chrome a `chromium_automation` bad bot in 5 of 5 runs each, but said "Not detected" for headful Brave in 5 of 5 (suspect score 29–40, against 36–52 for Chrome). That is one page on one day; verdicts vary from load to load, and a public test page isn't a protected site — see [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see) for these pages on other browsers and the lab cases for real sites.
