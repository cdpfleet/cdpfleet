# Watch a page live: its XHR calls and DOM changes over SSE and a signed webhook

**Medium** · 2026-11-10 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/watch-a-page-live)

> Keep a Chrome of yours on a page that updates itself (the ISS position, polled about once a second), and get every matching XHR response and every change to one element as it happens: on the job's SSE stream within a second, in signed `job.event` webhook batches, and stored with the job. Stop it, compare the three, and read what it cost, in five languages.

## The problem

Prices, scores, order books, queues, dashboards: the data on many pages arrives after the page has loaded, through the page's own XHR, fetch or WebSocket calls, and changes every second. Re-scraping such a page on a schedule misses most of it and costs a page load each time; reverse-engineering its API breaks when the site changes its tokens or headers. A [watch job](https://cdpfleet.com/docs/jobs#watch) does neither: it keeps one real browser on the page and reports what the page itself receives — the JSON responses, the WebSocket messages, the changes to an element — as they happen. This case runs one for real on a public page that updates itself without a reload, and checks the three ways the events reach you: live on SSE, as signed webhooks, and stored with the job.

## What we used, and why

| What | Why |
|---|---|
| `POST /v1/functions/watch` | `{url, browser: "chrome", duration: 300, reload_every: 15, filter: {url: ["*/w/ajax/realtime*"], selector: ".span3 table.table-striped", types: ["xhr", "fetch", "dom"]}}` → `202 {job_id}`. `duration` is only a safety net; the program stops the job itself. |
| `filter` | `url`: globs on the request URL — only the page's position calls, not its maps, fonts and analytics. `selector`: the element whose changes count (debounced 0.5 s), reported as its `text` and `html`. `types`: which kinds of events to send; the fourth, `ws` (WebSocket messages, Chromium-based browsers only), isn't used here. |
| `reload_every` | Reloads the page every 15 s. Here it is also what makes the DOM events start (see the takeaways). |
| `deliver: {webhook: {url, events: ["event", "done"]}}` | A `job.event` POST with a batch of events at most every 2 s, and `job.done` at the end, signed with the account's secret. The receiver in the programs is cdpfleet.com's own test inbox, which keeps the raw body and headers an hour and reads them back with GET; use your own public https endpoint. |
| `GET /v1/jobs/{id}/events` | Server-sent events: `event` messages with `{events: [...]}` the moment the service has them, plus `progress`, `part` and `done`. Read with a plain HTTP client in every language — no SSE library. |
| `DELETE /v1/jobs/{id}` | Stops the watch 40 s after it was started: `stopping`, then `stopped`, and the SSE stream ends with `done`. |
| `GET /v1/jobs/{id}/results` | The same events, stored with the job (kept 14 days). |
| `GET /v1/jobs/{id}` and `GET /v1/me/sessions` | A watch runs in your own browser session: `own_session: true`, `billed_seconds` in thread-seconds, and the session (engine, weight) in the account's session history. |

## How it works

1. Read the account's webhook secret and start the watch with a webhook to a fresh inbox URL.
2. Open the job's SSE stream at once and read it until `done`, counting events by type and timing each one from its `at` to its arrival; 40 s after the start, `DELETE` the job.
3. Read the job as it ended and its stored events; check they are exactly the events that arrived live.
4. Read the webhook deliveries: verify every signature on the raw body (and that a changed body fails), count the events in the `job.event` batches, the gaps between batches, and the `job.done`.
5. Parse the events: the ISS position from each XHR response (how far it moved, how fast), and the table's text from a DOM event.
6. Read the bill: `billed_seconds` against the job's running time, and the session behind it.

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
| Start | 202: Chrome on https://wheretheiss.at/, XHR/fetch matching */w/ajax/realtime* and changes under `.span3 table.table-striped`, reload every 15 s | 2026-11-10 |
| SSE | 38 events (25 xhr + 13 dom) in 17 messages; first XHR 17 s and first DOM change 27.9 s after the start; 0.55 s (max 1.01 s) from an event to its arrival | 2026-11-10 |
| Stop | DELETE at 40.1 s → stopping; job stopped 0.3 s later, SSE `done` with stopped | 2026-11-10 |
| Stored | 38 events (25 xhr + 13 dom), the same as arrived live: true | 2026-11-10 |
| Webhooks | 13 deliveries (12 job.event, 1 job.done), 13 of 13 signatures valid; 33 events in the batches, 5 stored events not sent; batches ≥ 2 s apart; job.done says interrupted | 2026-11-10 |
| The data | ISS from -42.451, 166.723 (6:23:53 pm PST) to -41.593, 168.359 (6:24:17 pm PST): 165.4 km of ground track, 6.93 km/s, 25 distinct positions in 25 responses | 2026-11-10 |
| DOM event | Time 6:24:04 pm PST Altitude 267.61 miles Velocity 17,121.66 mph Latitude -42.060081861191 Longitude 167.48044385043 The ISS is in daylight | 2026-11-10 |
| Cost | 40 thread-seconds for 40.4 s of running (own session: chrome, weight 1) | 2026-11-10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The whole loop works, in all five languages.** On 2026-11-10 each run started the watch, followed it on SSE, stopped it after 40 s and found the same events three ways: 38–49 of them per run (25–30 XHR responses, 13–19 DOM changes), stored with the job exactly as they had arrived live, and — all but the last few, see below — in signed webhooks.
- **Live means within a second.** The first event arrived 10.8–17.0 s after the `POST` (starting a Chrome and loading the page); after that each event reached the SSE stream a median 0.55–0.60 s and at most 1.01 s after it happened — the service sends events in batches of up to 1 s. The page asks for the position about once a second, and every response was a new position: 25–30 per run, the ISS moving 165–209 km at 6.86–6.94 km/s over the ground. The data comes from the page's own JSON, without parsing its HTML or calling its API yourself.
- **The filter decides what you pay attention to.** With `filter.url: ["*/w/ajax/realtime*"]` every reported call was the position request (`GET`, 200), and nothing else the page loads was reported. An XHR still in flight when the page reloads comes without `response_body` (0–2 per run): check for it before parsing.
- **DOM events start with the first reload.** A DOM event carries the element's `text` and `html` (about 1 KB here), but although the table changes with every response, the first one came 10.9–14.6 s after the first XHR in every run — at the first reload (`reload_every: 15`), not when the page loaded. Reported: the watch attaches its observer before the page has loaded, so it only starts with a reload; until that is fixed, set `reload_every` when you need DOM events.
- **Stop is immediate.** `DELETE` answered `stopping`; the job was `stopped` 0.3–1.4 s later and the SSE stream ended with `done` and status `stopped`.
- **Webhooks are the live feed, the stored events the record.** Each run received 12–16 `job.event` batches, at least 2.0 s apart and up to 7 events each, plus one `job.done`; all 75 deliveries had a valid signature (a changed body failed) and a unique delivery id, and each batch was signed within 2 s of its newest event. Two things to know, both reported: **the events of the last batch before a stop (1–5 per run, 5 of 5 runs) never came as a webhook** — they are in SSE and stored — and **`job.done` said `interrupted` while the job was `stopped`** (5 of 5). Take the complete record from `GET /v1/jobs/{id}/results` and the outcome from `GET /v1/jobs/{id}`.
- **A watch is paid by the second, not by the event: 40–41 thread-seconds for 40 s.** It runs in your own browser session (`own_session: true`; a `chrome` session of weight 1 in the session history), and `billed_seconds` equalled its running time (40.4–41.4 s) — 3,600 thread-seconds an hour for a headless Chrome, however many events. That is what makes it the tool for data that changes by the second; for a page that changes a few times a day, a [monitor](https://cdpfleet.com/docs/cases/monitor-end-to-end) costs a short scrape per check.
