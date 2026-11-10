# Crawl a whole site as a job: map, crawl, stop, resume, results, zip and a signed webhook

**Medium** · 2026-11-07 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/crawl-a-site-as-a-job)

> Map books.toscrape.com, crawl 100 pages of it as a background job, stop it part-way and resume it, then read the results as JSON lines and as a zip archive, and check the signed `job.done` and `job.archive` webhooks. Pages per minute and thread-seconds per page from the job's own bill, in five languages.

## The problem

One page is one call. A whole site is hundreds or thousands of pages, minutes or hours of work, and you don't want to hold a connection open for it, re-fetch what you already have after a stop, or poll forever to learn it's done. A [crawl job](https://cdpfleet.com/docs/jobs) runs in the background on your threads: you start it with one request, follow it, stop it and resume it where it left off, and get told by a signed webhook when it's finished. This case does the whole round trip on a site made for scraping practice, with a bounded crawl, and reads what it cost from the job itself.

## What we used, and why

| What | Why |
|---|---|
| `POST /v1/functions/map` | The instant map: the URLs a site lists in its sitemaps plus the links on its start page, in one call — a quick look at how big the site is before crawling it. |
| `POST /v1/functions/crawl` | `max_depth: 2`, `max_pages: 100`, `concurrency: 4`, `delay_ms: 250`, `formats: ["markdown"]` and an `extract` of each page's `h1` and price. Returns `202 {job_id}` at once; the job runs in pooled Camoufox tabs. |
| `deliver: {webhook: {url, events: ["done", "archive"]}, format: "archive"}` | A zip of every result built at the end, a `job.done` POST to your URL when the job ends, and a `job.archive` POST with the zip's `bytes` and download `url` when it is ready (only when `events` includes `"archive"`). The receiver in the programs is cdpfleet.com's own test inbox, which keeps deliveries an hour and reads them back with GET; use your own public https endpoint. |
| `GET /v1/jobs/{id}` | Polled every 3 s: `status`, `progress {done, failed, queued, total}`, `runs`, `concurrency`, `billed_seconds`, `results`, `archive`. |
| `DELETE /v1/jobs/{id}` → `POST /v1/jobs/{id}/resume` | Stop once 30 pages are done (`stopping`, then `stopped`, results kept), then continue as a second run from the job's checkpoint. |
| `GET /v1/jobs/{id}/results?cursor=` and `/results.jsonl` | The stored results at the stop, part by part; at the end, everything as one JSON-lines download. |
| `GET /v1/jobs/{id}/archive` | `{status, bytes, url}`: the `url` downloads the zip straight from storage, without your key. Read with the standard zip library in each language (Node: a short central-directory reader and `zlib`). |
| `GET /v1/me/webhook-secret` | The account's `whsec_…`; each delivery's `x-cdpfleet-signature: t=…,v1=…` must equal HMAC-SHA256(secret, `"<t>.<raw body>"`). |

## How it works

1. Map the site with one instant call and count the URLs it found and where they came from.
2. Start the crawl job with a webhook and `format: "archive"`; poll it until 30 pages are done, then stop it and time how long it takes to reach `stopped`. Read the results stored so far and what the webhook inbox has received.
3. Resume it and poll until the second run has ended.
4. Download `results.jsonl`: count the lines, the distinct URLs, the pages with Markdown, the book pages with a price, the depths, and the URLs the second run added.
5. Wait for the archive, download the zip and list it: the Markdown and `extract.json` files per page, `index.jsonl`, `errors.jsonl`, `job.json`.
6. Read the deliveries from the webhook inbox, check every signature with the account's secret, and download the zip again from the link in `job.archive`.
7. Work out pages per minute and thread-seconds per page from `billed_seconds` and `concurrency`.

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
| Map | 73 URLs in 6.4 s (sitemap 0, links 73); 51 category pages | 2026-11-10 |
| Crawl job | 202: max_depth 2, max_pages 100, concurrency 4, delay 250 ms, Markdown + extract | 2026-11-10 |
| Stop | DELETE at 30 pages → stopping; stopped 3.1 s later with 33 results stored; webhooks by then: job.done stopped | 2026-11-10 |
| Resume | 202 running → done after 2 runs; the second run added 67 new pages | 2026-11-10 |
| results.jsonl | 100 lines, 100 distinct URLs, 811 KB; 100 pages with Markdown (status 200 × 96, 407 × 4), 44 books with title and price, 0 failed | 2026-11-10 |
| Zip archive | 252 KB: 203 files, 100 index.md + 100 extract.json, index.jsonl 100 lines | 2026-11-10 |
| Webhooks | job.done (stopped) signature ok, job.done (done) signature ok, job.archive (ready) signature ok | 2026-11-10 |
| Speed | 100 pages in 246 s of running time: 24.4 pages/min | 2026-11-10 |
| Cost | 984 thread-seconds (9.8 per page) at concurrency 4 | 2026-11-10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The whole round trip works, in all five languages.** On 2026-11-10 (re-run after the Functions fixes of that day) each run mapped the site, started the crawl, stopped it, resumed it and finished with exactly 100 pages: 100 lines in `results.jsonl`, 100 distinct URLs, every one with Markdown, none fetched twice. The depths were the same every time (1 start page, 73 at depth 1, 26 at depth 2); which 26 differed, so the book pages with a title and price came out 44 or 45.
- **Map first: 73 URLs in 4.6–10.6 s.** books.toscrape.com has no sitemap, so the instant map is the start page's links (`sources: {sitemap: 0, links: 73}`, and `x-cdpfleet-partial: fetch_errors` for the missing robots/sitemap files): 51 category pages, 20 books, the home page and page 2. For a site with sitemaps the same call lists up to 5,000 URLs; `"job": true` maps deeper.
- **Stop is quick and loses nothing; resume continues, it doesn't restart.** `DELETE` at 30–38 pages answered `stopping`; the job was `stopped` 3.1–12.5 s later with 33–45 results stored (the pages already in flight finish, at most 7 more). `POST …/resume` answered `202 running`, the second run fetched only URLs the first hadn't (55–67 of them) and the job ended `done` with `runs: 2`. No archive is built at a stop; it is built once, at the end.
- **`progress` counts against `max_pages`.** At the stop `progress` read `total: 100` with 55–67 `queued`, and at the end `{done: 100, failed: 0, queued: 0, total: 100}`: a progress bar can be drawn straight from it. (Until 2026-11-10 `total` counted every link discovered, 1,600–1,800 here.)
- **The zip is about a third of the JSON lines.** `results.jsonl` was 801–811 KB (Markdown inside JSON strings); the archive 250–252 KB with 203 files: an `index.md` and an `extract.json` per page, plus `index.jsonl` (100 lines), `errors.jsonl` and `job.json`. Use the JSON lines to stream results into code, the zip to hand a folder of Markdown to an LLM pipeline or keep it.
- **Three signed webhooks, each saying what happened.** Every run received exactly three deliveries, all with a valid `x-cdpfleet-signature`, the job's id and a uuid delivery id: `job.done` with `data.status: "stopped"` at the stop, `job.done` with `status: "done"` and the final progress (signed in the same second as `ended_at`), and `job.archive` 0–1 s later with the zip's `bytes` and a download `url` — the same size as `GET …/archive`, and the link downloaded the same zip without a key. `job.archive` comes only because `events` asks for `"archive"`; with the default `["done"]` you get the two `job.done`. (Before the 2026-11-10 fix the stop's `job.done` said `interrupted`, and `job.archive` came unasked with only `{status}`.)
- **Check `markdown`, not only `status`.** 2–4 of the 100 pages in three of the five runs came back with `status: 407` (Proxy Authentication Required — not what the site answered) while their Markdown and extracted price were complete (reported to Functions). Treat a page as fetched when it has its Markdown, and look at `error` for the ones that weren't.
- **24–29 pages a minute at concurrency 4, 8.4–9.8 thread-seconds a page.** The two runs together ran 210–246 s for 100 pages and were billed 840–984 thread-seconds — `billed_seconds` is exactly `concurrency` × running time (98–99% of the time the client saw the job running; the pause between stop and resume costs nothing). At this pace 1,000 pages take about 2.3–2.7 thread-hours of your plan; the Community proxy (the default) adds nothing. A page took 8–10 s in a pooled tab including the 250 ms delay, so the way to go faster is more `concurrency`, not a shorter delay.
