# Scrape a catalogue page by page: structured data from rendered HTML

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/structured-extraction)

> Extract every book on two pages of a catalogue — title, price, star rating and availability — as clean JSON, parsed straight from the rendered DOM.

## The problem

A catalogue page shows 20 items per page with prices buried in formatted strings ("£51.77"), star ratings encoded in CSS class names ("star-rating Three") and availability wrapped in whitespace. You want structured records — numbers, integers, booleans — not markup. And you want the next page too, which means clicking through pagination.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session, nothing browser-specific. |
| `page.evaluate` with `querySelectorAll` | Extract all 20 books at once in a single round trip. Parse prices and ratings in the same JS call, no extra traffic. |
| `parseFloat(text.replace(/[^0-9.]/g, ''))` | Strips the currency symbol and returns a number. |
| CSS class list on `.star-rating` | The class name is the word form of the rating (One through Five) — a lookup table turns it into 1–5. |
| `li.next a` click | Follow pagination the way a user would: click "next", wait for the new page, extract again. |

## How it works

1. Open the front page of books.toscrape.com and extract all 20 books as structured objects: title, price (number), star rating (1–5) and in-stock status (boolean).
2. Click the "next" button to go to page 2.
3. Extract the 20 books on page 2 the same way.
4. Print one JSON object: pages scraped, total book count, the full array, and how long it took.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Field | Value |
|---|---|
| Pages scraped | 2 |
| Total books | 40 |
| Prices | £12.84 – £57.25, all parsed to numbers |
| Star ratings | 1 – 5, all parsed from CSS classes |
| Availability | all in stock (boolean `true`) |
| Time | 3.2 s for both pages |

Full output: [output.json](output.json).

## Takeaways

- **One `evaluate` call per page extracts all 20 books** — parsing prices, ratings and availability in-browser avoids 20 round trips and lets plain string operations work on the live DOM text.
- **CSS-class ratings are a common pattern:** the word "Three" in `star-rating Three` is more reliable than counting filled-star icons, and it survives layout changes.
- **Pagination via click, not URL construction:** clicking `li.next a` uses the site's own link, so it works even if the URL scheme changes — and it exercises the same code path a user would.
- **40 books in 3 seconds through a residential proxy** — most of the time is two page loads; the extraction itself is sub-millisecond.
