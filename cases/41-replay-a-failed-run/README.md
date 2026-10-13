# A scrape broke: find out why from its session recording

**Easy** · 2026-10-13 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/replay-a-failed-run)

> A scraper dies with a timeout. Its session was recorded anyway: fetch the video by session id and see the page it failed on, with no tracing set up in advance.

## The problem

A scraper that ran fine for weeks fails with "Timeout 5000ms exceeded" and nothing else. Was the selector wrong, did the site change, was there a captcha, a cookie banner, an error page? Playwright tracing would tell you — if you had turned it on before the run. cdpfleet [records sessions](https://cdpfleet.com/docs/recordings) without any setup: headful sessions are recorded from their display by default, so the video of the failed run already exists. Here a scraper breaks on purpose, and we fetch, check and delete its recording through the API.

## What we used, and why

| What | Why |
|---|---|
| `"headless": false` | Headful, the default. Headful sessions are recorded from the browser's display; headless ones only with `cdp` or `live_view`. |
| `viewport: null` | The page fills the whole display instead of a 1280×720 window on it — so it fills the video too (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize.NoViewport`, Go `NoViewport`). |
| [quotes.toscrape.com](https://quotes.toscrape.com/) | A public practice site with ten listing pages. The scraper starts on page 8 and assumes every page has a "Next" link — page 10, the last one, doesn't. |
| `GET /v1/me/recordings` | Lists the account's recordings with their `session_id`, duration, size and resolution. A recording appears once the node has uploaded it, after the session ended. |
| `GET /v1/me/recordings/{id}/url` | A short-lived private link to the MP4. `DELETE /v1/me/recordings/{id}` deletes it. |
| `"record": false` | A second session launched with recording off, to show it gets no video. |

## How it works

1. Launch a session with `"record": false`, load the site, close it.
2. Launch the scraper's session (headful, recorded) and keep its `sessionId`. From page 8, collect the quotes and follow the "Next" link — 5 seconds to find it, then load its page — until the link can't be found; note the time since launch, the URL and what the page has.
3. Close the browser, which ends the session. Poll `GET /v1/me/recordings` until a recording with that `session_id` appears.
4. Get its link, download the MP4, compare its duration with the moment of failure, then delete it.
5. Wait another minute and check that the `record: false` session never got a recording.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-13):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

|  | Measured |
|---|---|
| Scrape | 30 quotes from pages 8–10, then: timeout waiting for li.next a |
| Failed at | 10.4 s after launch, on https://quotes.toscrape.com/page/10/ (0 "Next" links, 1 "Previous") |
| Recording appeared | 5 s after the session ended |
| Video | 11 s, 854×480, 57443 bytes, video/mp4 |
| Failure inside the video | yes |
| record: false session | no recording |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The error alone said only "timeout waiting for `li.next a`".** At that moment the browser was on `/page/10/` with a "Previous" link and no "Next" — the last page, not a broken selector. The failure came 10.4–13.2 s after launch, the browser closed a moment later, and the videos were 11–14 s long — so their last seconds were recorded with that page on screen.
- **No setup:** the scraper's code does nothing about recording. Its headful session was recorded by default, and in every run the video was listed under its `session_id` on the second check, 5 seconds after the session ended (the first, right after closing, found nothing yet).
- **What you get:** an MP4 at 854×480 — the free tier's quality ([plans](https://cdpfleet.com/docs/recordings) add 720p and 1080p) — of 57–62 KB for these 11–14 s, behind a private link valid for 15 minutes. `DELETE /v1/me/recordings/{id}` deletes it (it answered 200 in every run).
- **`"record": false` really is off:** that session never got a recording, checked a minute after the other video had arrived.
- **Keep "find the element" and "load the page" on separate timeouts.** Reading the "Next" link with a 5-second timeout and loading its page with the normal one means the error names what was missing; a click that also waits for a slow page load can time out mid-navigation and send you looking at the wrong page.
