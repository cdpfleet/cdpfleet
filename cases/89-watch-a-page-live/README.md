# Watch a page live: its XHR calls, DOM changes and WebSocket messages over SSE and a signed webhook

**Medium** · 2026-11-10 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/watch-a-page-live)

> Keep a Chrome of yours on a page that updates itself (the ISS position, polled about once a second), and get every matching XHR response and every change to one element as it happens: on the job's SSE stream within a second, in signed `job.event` webhook batches, and stored with the job. Stop it, compare the three, read what it cost — and capture a second page's WebSocket messages both ways. In five languages.

## The problem

Prices, scores, order books, queues, dashboards: the data on many pages arrives after the page has loaded, through the page's own XHR, fetch or WebSocket calls, and changes every second. Re-scraping such a page on a schedule misses most of it and costs a page load each time; reverse-engineering its API breaks when the site changes its tokens or headers. A [watch job](https://cdpfleet.com/docs/jobs#watch) does neither: it keeps one real browser on the page and reports what the page itself receives — the JSON responses, the WebSocket messages, the changes to an element — as they happen. This case runs one for real on a public page that updates itself without a reload, checks the three ways the events reach you — live on SSE, as signed webhooks, and stored with the job — and then watches a second page that gets its data over a WebSocket.

## What we used, and why

| What | Why |
|---|---|
| `POST /v1/functions/watch` | `{url, browser: "chrome", duration: 300, filter: {url: ["*/w/ajax/realtime*"], selector: ".span3 table.table-striped", types: ["xhr", "fetch", "dom"]}}` → `202 {job_id}`. `duration` is only a safety net; the program stops the job itself. No `reload_every`: the page updates itself. |
| `filter` | `url`: globs on the request (or WebSocket) URL — only the page's position calls, not its maps, fonts and analytics. `selector`: the element whose changes count (debounced 0.5 s), reported as its `text` and `html`. `types`: which kinds of events to send — `xhr`, `fetch`, `dom`, and `ws` for WebSocket messages (Chromium-based browsers only). |
| `deliver: {webhook: {url, events: ["event", "done"]}}` | A `job.event` POST with a batch of events at most every 2 s, and `job.done` at the end, signed with the account's secret; without `"event"` in `events` a watch sends only `job.done`. The receiver in the programs is cdpfleet.com's own test inbox, which keeps the raw body and headers an hour and reads them back with GET; use your own public https endpoint. |
| `GET /v1/jobs/{id}/events` | Server-sent events: `event` messages with `{events: [...]}` the moment the service has them, plus `progress`, `part` and `done`. Read with a plain HTTP client in every language — no SSE library. |
| `DELETE /v1/jobs/{id}` | Stops the watch 40 s after it was started: `stopping`, then `stopped`, and the SSE stream ends with `done`. |
| `GET /v1/jobs/{id}/results` | The same events, stored with the job (kept 14 days). |
| `GET /v1/jobs/{id}` and `GET /v1/me/sessions` | A watch runs in your own browser session: `own_session: true`, `billed_seconds` in thread-seconds, and the session (engine, weight) in the account's session history. |
| A second watch: `{url: "https://mempool.space/", filter: {url: ["*/api/v1/ws*"], types: ["ws"]}}` | mempool.space pushes Bitcoin mempool statistics to its page over one WebSocket. No `browser`: a watch starts Chrome by default. Stopped after 45 s; its stored events are read with `GET /v1/jobs/{id}/results`. |

## How it works

1. Read the account's webhook secret and start the watch with a webhook to a fresh inbox URL.
2. Open the job's SSE stream at once and read it until `done`, counting events by type and timing each one from its `at` to its arrival; 40 s after the start, `DELETE` the job.
3. Read the job as it ended and its stored events; check they are exactly the events that arrived live.
4. Read the webhook deliveries: verify every signature on the raw body (and that a changed body fails), count the events in the `job.event` batches, the gaps between batches, and the `job.done`.
5. Parse the events: the ISS position from each XHR response (how far it moved, how fast), and the table's text from a DOM event.
6. Start the WebSocket watch, stop it after 45 s, and read its messages: the ones the page sent, the ones it received, and what they carry.
7. Read the bill: `billed_seconds` against the job's running time, and the sessions behind both watches.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-10):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Step | What happened | Measured |
|---|---|---|
| Start | 202: Chrome on https://wheretheiss.at/, XHR/fetch matching */w/ajax/realtime* and changes under `.span3 table.table-striped` | 2026-11-10 |
| SSE | 60 events (30 xhr + 30 dom) in 24 messages; first XHR 12.1 s and first DOM change 12.1 s after the start; 0.53 s (max 1.01 s) from an event to its arrival | 2026-11-10 |
| Stop | DELETE at 40.1 s → stopping; job stopped 0.5 s later, SSE `done` with stopped | 2026-11-10 |
| Stored | 60 events (30 xhr + 30 dom), the same as arrived live: true | 2026-11-10 |
| Webhooks | 17 deliveries (16 job.event, 1 job.done), 17 of 17 signatures valid; 60 events in the batches, 0 stored events not sent; batches ≥ 2 s apart until the stop; job.done says stopped | 2026-11-10 |
| The data | ISS from -24.513, 71.937 (2:15:23 am PST) to -23.144, 73.19 (2:15:52 am PST): 198.5 km of ground track, 6.92 km/s, 30 distinct positions in 30 responses | 2026-11-10 |
| DOM event | Time 2:15:23 am PST Altitude 262.23 miles Velocity 17,135.44 mph Latitude -24.513271274056 Longitude 71.937013051503 The ISS is in daylight | 2026-11-10 |
| WebSocket | https://mempool.space/: 4 messages sent, 31 received (27 mempoolInfo, 1 blocks, 1 rbfLatestSummary, 1 (cut at 64 KB), 1 live-2h-chart), first 18.1 s after the start; chrome by default, 45 thread-seconds | 2026-11-10 |
| Cost | 40 thread-seconds for 40.6 s of running (own session: chrome, weight 1) | 2026-11-10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The whole loop works, in all five languages.** On 2026-11-10 (re-run after the Functions fixes of that day) each run started the watch, followed it on SSE, stopped it after 40 s and found the same events three ways: 50–60 of them per run (25–30 XHR responses and as many DOM changes, give or take one), stored with the job exactly as they had arrived live, and every one of them in a signed webhook.
- **Live means within a second.** The first event arrived 8.3–17.3 s after the `POST` (starting a Chrome and loading the page); after that each event reached the SSE stream a median 0.52–0.55 s and at most 1.02 s after it happened — the service sends events in batches of up to 1 s. The page asks for the position about once a second, and every response was a new position: 25–30 per run, the ISS moving 164–226 km at 6.92–6.94 km/s over the ground. The data comes from the page's own JSON, without parsing its HTML or calling its API yourself.
- **The filter decides what you pay attention to.** With `filter.url: ["*/w/ajax/realtime*"]` every reported call was the position request (`GET`, 200, with its `response_body`), and nothing else the page loads was reported.
- **DOM events start with the page.** A DOM event carries the element's `text` and `html` (about 1 KB here); the first one came in the same second as the first XHR in every run, and then one per position update — no `reload_every` needed. (Before the 2026-11-10 fix the observer only started at the first reload, 11–15 s later; `reload_every` is now only for pages that need refreshing.)
- **Stop is immediate.** `DELETE` answered `stopping`; the job was `stopped` 0.4–0.7 s later and the SSE stream ended with `done` and status `stopped`.
- **Webhooks carry everything, in batches.** Each run received 13–16 `job.event` batches with up to 6 events each, plus one `job.done` with `status: "stopped"`; all 79 deliveries had a valid signature (a changed body failed) and a unique delivery id, and each batch was signed within 1.3 s of its newest event. Batches came at least 2.0 s apart until the stop; the last one, flushed at the stop, followed the previous after 0.4–2.0 s. Every stored event was also in a webhook — the last batch before a stop is no longer lost.
- **WebSocket messages, both ways.** On mempool.space the watch captured the page's own subscription — the 4 messages it sent (`{"action":"init"}`, `{"action":"want","data":["blocks","stats","mempool-blocks","live-2h-chart"]}` and two more) — and 24–38 messages it received in 45 s, the first 6.4–24.0 s after the `POST`: one `blocks` snapshot and then a `mempoolInfo` update about every second (the mempool at 75,815–77,337 transactions). `data` is the frame's text, cut at 64 KB: one large message per run no longer parsed as JSON — check before you parse. The job ran in Chrome without asking for it. One more attempt (C#, not kept) ended `failed` with `error: "navigation_failed"` after 31 s, and was billed those 31 s: check a job's `status` and `error`, not only its events.
- **A watch is paid by the second, not by the event: 40 thread-seconds for 40 s.** It runs in your own browser session (`own_session: true`; a `chrome` session of weight 1 in the session history), and `billed_seconds` matched its running time (40.4–40.7 s); the 45 s WebSocket watch was billed 45–46 — 3,600 thread-seconds an hour for a headless Chrome, however many events. That is what makes it the tool for data that changes by the second; for a page that changes a few times a day, a [monitor](https://cdpfleet.com/docs/cases/monitor-end-to-end) costs a short scrape per check.
