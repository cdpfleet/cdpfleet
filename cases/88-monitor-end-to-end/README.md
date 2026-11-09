# A monitor end to end: create it, trigger a change, verify the signed webhook, read the diff and history

**Medium** · 2026-11-09 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/monitor-end-to-end)

> Watch one element of a page that changes on every load, trigger checks with `POST …/run`, receive the `monitor.change` webhook and verify its HMAC signature, check its diff against the API's history and snapshots, see what each check cost, then pause and delete the monitor. Timed from run to webhook, in five languages.

## The problem

Watching a page for changes sounds like a cron job and a `diff`, until you need it to load in a real browser, ignore the parts that change on every visit, keep the history, and tell your code (or your agent) the moment something real moved — with a request your endpoint can trust. A [monitor](https://cdpfleet.com/docs/monitors) does that on cdpfleet's side: it checks the page on a schedule, compares the text with the last snapshot and POSTs a signed `monitor.change` webhook with the diff. This case runs one for real on a page that is different on every load, so it can be made to change on demand, and checks every piece a receiver relies on: the signature, the delivery id, that the webhook's diff is the one the API keeps, and what the checks cost.

## What we used, and why

| What | Why |
|---|---|
| `POST /v1/monitors` | `{url, format: "text", selector: ".quote", remove: [".tags"], ignore: ["\\(about\\)"], every: "1h", deliver: {webhook: {url}}}`: compare only the quote and its author — `remove` drops the tags element before the text is taken, `ignore` deletes the "(about)" link label from the text. `every: "1h"` is the real schedule (15 minutes is the shortest on shared plans); the programs don't wait for it. |
| `POST /v1/monitors/{id}/run` | Check now: answers `202 queued` and the check starts at once. The first check stores a baseline, later ones report `unchanged` or `changed`. |
| `GET /v1/monitors/{id}` | Polled every 0.5 s: `checks`, `changes`, `last_status`, `last_error`, `last_run_at`, `next_run_at`. |
| `deliver.webhook` and `GET /v1/me/webhook-secret` | A `monitor.change` POST with headers `x-cdpfleet-event`, `x-cdpfleet-delivery` (a uuid, the same on every retry) and `x-cdpfleet-signature: t=…,v1=…` = HMAC-SHA256(whsec, `"<t>.<raw body>"`). The receiver in the programs is cdpfleet.com's own test inbox, which keeps the raw body and headers an hour and reads them back with GET; use your own public https endpoint. |
| `GET /v1/monitors/{id}/changes` and `/changes/{change_id}` | The history (newest first, with a diff preview), and one change in full: the diff, `added`/`removed` line counts, and links to the text before and after. |
| `PATCH /v1/monitors/{id}` | `{active: false}` pauses it: `next_run_at` becomes null, the history stays. |
| `GET /v1/me/sessions` | Each check is one scrape the Functions service runs as you; it has no `x-call-id` you can see, but it is in the session history as engine `function:scrape`, with its start, end and weight. |
| `DELETE /v1/monitors/{id}` | In a `finally` block, so a failed run doesn't leave a monitor checking every hour. |

## How it works

1. Read the account's webhook secret and create the monitor with a webhook to a fresh inbox URL; check it is in `GET /v1/monitors`.
2. Trigger a check and poll the monitor until it has run: the baseline. Trigger more checks (at most 5) until one reports `changed`, polling the webhook inbox at the same time.
3. Verify the `monitor.change` delivery: the signature over the raw body (and that a body changed by one space fails), the timestamp, the delivery id, the event header.
4. Fetch the same change from the API and compare: the diff byte for byte, the line counts; download the "after" text both from the API's link and from the webhook's `snapshot_url` and compare them.
5. Read the history and the monitor's counters, then pause it with `PATCH`.
6. Find each check's scrape in the session history and add up its thread-seconds.
7. Delete the monitor and check it is gone (`404`).

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Step | What happened | Measured |
|---|---|---|
| Create | 201: every 1h, text of `.quote`, without .tags, ignoring \(about\); webhook set, listed true | 2026-11-09 |
| Checks | #1 baseline in 10.8 s, #2 changed in 18.7 s | 2026-11-09 |
| Webhook | 1 delivery: monitor.change, signature valid, tampered body rejected, delivery id a uuid; seen 19.1 s after the run request | 2026-11-09 |
| Diff | +2 −2 lines, 288 bytes; same as the API: true; snapshot_url = after text: true; tags or "(about)" in it: false | 2026-11-09 |
| Now reads | “He's like a drug for you, Bella.” — by Stephenie Meyer | 2026-11-09 |
| History | 1 change (newest = the webhook's: true), 2 checks; next scheduled check 60 min after the last | 2026-11-09 |
| Cost | 10.7 s + 18.5 s = 29.2 thread-seconds (function:scrape, weight 1) | 2026-11-09 |
| Pause, delete | PATCH active:false → 200, next_run_at null; DELETE → 200, then GET → 404 | 2026-11-09 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The whole loop works, in all five languages.** On 2026-11-09 each run created the monitor, got `baseline` from the first `POST …/run` and `changed` from the second, received exactly one webhook — `monitor.change`, none for the baseline — verified it, matched it to the history, paused the monitor and deleted it (`GET` then answered `404`).
- **The webhook arrives with the result, 7.7–19.1 s after the run request.** A check took 7.0–18.7 s from `POST …/run` to `last_run_at` (one browser load of the page in a pooled tab), and the webhook was in the inbox on the same 0.5 s poll that first showed `changed`. Its signature timestamp was within a second of the change's `at`: main sends it the moment the change is recorded, not on a delivery schedule. So the time to news is the time of one page load.
- **Verify the raw body, then trust it.** In every run `x-cdpfleet-signature` was HMAC-SHA256 of `"<t>.<raw body>"` with the secret from `GET /v1/me/webhook-secret`, the same body with one added space failed the check, `x-cdpfleet-event` was `monitor.change` and `x-cdpfleet-delivery` a uuid (use it to drop retried duplicates). The body is `{type, created_at, data}`; `data` holds `monitor_id`, `change_id`, `url`, `at`, `added`, `removed`, `diff` and `snapshot_url`.
- **The webhook carries the whole story; the API has the same thing.** The webhook's `diff` was byte for byte the one `GET /v1/monitors/{id}/changes/{change_id}` returns, with the same line counts, and its `snapshot_url` downloaded exactly the change's "after" text. Each diff was a 7-line unified diff, 194–646 bytes: `+2 −2`, the old quote and author out, the new ones in.
- **Compare the part that matters.** With `selector: ".quote"`, `remove: [".tags"]` and `ignore: ["\\(about\\)"]` the snapshot was two lines (the quote, "by" the author) and no diff contained the tags or the "(about)" label. Use `remove` for whole elements (it works on the page) and `ignore` for patterns inside the text (dates, counters).
- **A check costs one short scrape: 6.8–18.5 thread-seconds.** Each check was one `function:scrape` session of weight 1 in `GET /v1/me/sessions`, starting right after the run request; the two checks of a run came to 13.9–29.2 thread-seconds. An hourly monitor is 24 such checks a day, a few thread-minutes. The check has no `x-call-id` of its own, so the programs match it to the session by time.
- **Running a check by hand also resets the schedule.** A new monitor's first check is due at once (`next_run_at` = `created_at`), and after every check — `POST …/run` included — `next_run_at` was 60 minutes after it. `PATCH {active: false}` set `next_run_at` to null and kept the history.
