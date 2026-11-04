# Behaviour scoring: Camoufox humanize on and off vs plain Chrome clicks on a public bot-test form

**Medium** · 2026-11-04 · Camoufox, Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/behaviour-scoring-humanize)

> A public behavioural bot-test page scores how a demo login form was filled in: clicks, keystrokes, timing. Camoufox with `humanize` off, on and slowed down, and headful Chrome with plain Playwright clicks, each filled it the same way three times, typing fast and typing paced. What the page flagged, what it measured, and whether Google's reCAPTCHA v3 demo moved after real cursor moves and a click. Dated 2026-11-04.

## The problem

Fingerprint checks look at what a browser is; behaviour checks look at what it does — whether the cursor travelled before a click, where the click landed, whether the events were trusted, how fast the keys came. Camoufox's `humanize` option moves the cursor along a curved path instead of jumping, and it is on by default. Does that change what a behaviour-scoring page concludes? [Case 68](https://cdpfleet.com/docs/cases/captcha-scores-by-browser) loaded Google's reCAPTCHA v3 demo with nothing clicked and every setup scored 0.9, so that demo alone can't answer it. This case fills in a public demo form built for exactly this test, with the same actions in every setup, and records the page's verdict, the flags it raised and what it measured. It is a dated measurement on public test pages, not an evasion guide.

## What we used, and why

| What | Why |
|---|---|
| Four setups | Camoufox (based on Firefox source; not affiliated with Mozilla) with `humanize: false`, `humanize: true` (the default: a curved cursor path of up to 1.5 s) and `humanize: 3` (up to 3 s), all `headless: true`; and headful Google Chrome as the reference, driven by plain Playwright. Every launch uses `record: false`. |
| `proxy`: `cdpfleet-resi-country-us-session-<id>` | One **sticky** residential exit for the whole run, shared by every setup, so a difference between rows is the browser and how it was driven, not the IP. The form page says it uses no IP reputation; the exit only keeps the network the same. |
| deviceandbrowserinfo.com's behavioural bot test | A public demo login form (`/are_you_a_bot_interactions`) that records clicks, keystrokes and timing; on submit it posts them with a fingerprint to its own endpoint and shows "You are human" or "You are a bot" with the flags behind it. The page says repeated tests don't affect the result. Any email and password are fine; nothing is logged in to. |
| `page.click()` and `keyboard.type()` | The same five actions in every setup: click the email field, type `lab81@example.com`, click the password field, type an 11-character password, click Login. **Fast** typing sends keys with no delay, **paced** with 120 ms between keys. `humanize` changes how the cursor gets to each click target, not where the click lands or how keys are typed. |
| `page.on("request")` | Reads the body the page posts to its own endpoint, so the table can show what the page itself measured: time from the first field interaction to submit, whether each click hit the exact centre of its target, untrusted events, its CDP mouse-coordinate check, the median gap between keys. Nothing is changed or blocked. |
| `page.evaluate()` | Counts `mousemove` events on the form page, and reads the verdict the page prints. |
| Google's reCAPTCHA v3 demo | In each session: load the demo, wait for the score it asks for on load, move the cursor twice and click its "Try again" link, read the score of the reloaded page. |

## How it works

1. Three rounds; in each, the four setups one after another — one session at a time, never in parallel.
2. In each session, open the behavioural test form in a fresh context, fill it in with fast typing and submit; read the verdict (up to 30 s). Then the same in another fresh context with paced typing.
3. In a third fresh context, load the reCAPTCHA v3 demo, move the cursor, click "Try again", and read the new score.
4. A page load that fails on the network is tried once more; a verdict is never retried.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-04):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Fast typing: verdict | Paced typing: verdict | Time to fill, median (fast · paced) | mousemove events per form, median | Clicks on the exact centre | reCAPTCHA v3 demo after moves + click | Measured |
|---|---|---|---|---|---|---|---|
| Camoufox, humanize: false | bot 1/3 (suspiciousClientSideBehavior 1, superHumanSpeed 1) | human 3/3 | 1.9 s · 4.3 s | 3 | 18/18 | 0.9 · 0.9 · 0.9 | 2026-11-04 |
| Camoufox, humanize: true (default) | human 3/3 | human 3/3 | 3.2 s · 5.2 s | 72 | 18/18 | 0.9 · 0.9 · 0.9 | 2026-11-04 |
| Camoufox, humanize: 3 | human 3/3 | human 3/3 | 3.1 s · 5.0 s | 67 | 18/18 | 0.9 · 0.9 · 0.9 | 2026-11-04 |
| Google Chrome, headful | bot 3/3 (hasInconsistentTimingResolution 3, isAutomatedWithCDP 3, isAutomatedWithCDPInWebWorker 3, hasSuspiciousWeakSignals 3, suspiciousClientSideBehavior 3, superHumanSpeed 3) | bot 3/3 (hasInconsistentTimingResolution 3, isAutomatedWithCDP 3, isAutomatedWithCDPInWebWorker 3, hasSuspiciousWeakSignals 3, suspiciousClientSideBehavior 3) | 192 ms · 4.1 s | 3 | 18/18 | 0.9 · none · 0.9 | 2026-11-04 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Plain Playwright in headful Chrome was a bot in all 30 runs, and paced typing didn't change that.** On 2026-11-04 the page raised `isAutomatedWithCDP` (and its web-worker variant) every time, which is about the browser being driven over CDP, not about the cursor; `suspiciousClientSideBehavior` came with it in all 30 runs, paced or not. `superHumanSpeed` appeared in all 15 fast runs (form filled in a median 0.3 s) and in none of the 15 paced ones (about 4 s).
- **`humanize` changed how the cursor travelled, not where it clicked.** With `humanize: false` the page saw 3 `mousemove` events per form, one per click; with `humanize: true` it saw 43–86 and with `humanize: 3` 49–90. Every click in every setup (354 of 354 measured) still landed within 1 px of the exact centre of its target, because Playwright aims at the centre and `humanize` only shapes the path. The page records centre clicks; on their own they didn't make it say "bot". No run had an untrusted event or tripped its CDP mouse-coordinate check.
- **With paced typing, Camoufox raised no behaviour flag in 44 of 44 runs — `humanize` off, on or 3.** The verdict was "human" in 43; the one "bot" (humanize on) came from a single fingerprint flag, `hasInconsistentTimingResolution`, with no behaviour flag at all.
- **What the page flagged in Camoufox was speed.** With fast typing, `humanize: false` got `suspiciousClientSideBehavior` + `superHumanSpeed` in 5 of 14 runs; `humanize: true` and `humanize: 3` in 0 of 15 each. Every flagged run had filled the form in under 0.7 s (time from the first field click to submit, as the page measures it); no run that took 1.5 s or longer was flagged. `humanize` doesn't change typing, but its cursor travel made filling the form slower: median 1.4 s off, 2.6 s with `humanize: true` and 2.6 s with `3` (fastest humanized run: 1.0 s).
- **Google's reCAPTCHA v3 demo still said 0.9 after real cursor moves and a click.** 56 of 60 sessions got a score after the reload, every one 0.9, for all four setups, the same as the score each got before moving (55 of 60, all 0.9) and as [case 68](https://cdpfleet.com/docs/cases/captcha-scores-by-browser) with nothing touched. The demo doesn't separate these setups; a page built to score behaviour did.
- **A demo page's verdict is not a real site's.** This page scores one form with fixed rules, says it ignores IP reputation, and lets you repeat the test. Real protections weigh far more — see the lab cases from [Cloudflare](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through) to [Kasada and AWS WAF](https://cdpfleet.com/docs/cases/kasada-and-aws-waf-by-browser) and the [scoreboard](https://cdpfleet.com/bot-protection/scoreboard).
