# Scrape a catalogue page by page: structured data from rendered HTML

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/structured-extraction)

> Every book on two pages of a catalogue — title, price, star rating and availability — as clean JSON, parsed from the rendered DOM in one evaluate call per page.

## The problem

A catalogue page shows 20 items with prices in formatted strings ("£51.77"), star ratings encoded in CSS class names ("star-rating Three") and availability wrapped in whitespace. You want records with real numbers, integers and booleans — and you want the next page too, the way a visitor gets there.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session, nothing browser-specific. |
| `page.evaluate` with `querySelectorAll` | All 20 books in one round trip, parsed inside the page. |
| `parseFloat(text.replace(/[^0-9.]/g, ""))` | Strips the currency symbol and returns a number. |
| The class list of `.star-rating` | The rating is a word (One … Five) in a class name; a lookup table turns it into 1–5. |
| Clicking `li.next a` | Follows pagination with the site's own link instead of building URLs. |

## How it works

1. Open the first page of books.toscrape.com and extract all 20 books: title, price (number), rating (1–5), in stock (boolean).
2. Click "next" and wait for page 2.
3. Extract page 2 the same way and print one JSON document with the count, the records and the time taken.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Pages | Books | Prices | Ratings | In stock | Seconds |
|---|---|---|---|---|---|
| 2 | 40 | £12.84 – £57.25 | 1, 2, 3, 4, 5 | 40/40 | 10.18 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One `evaluate` per page extracts all 20 records** — parsing in the page avoids 20 round trips over the session's connection.
- **Ratings in class names are a common pattern:** the word in `star-rating Three` is sturdier than counting star icons.
- **Paginate with the site's own link:** clicking `li.next a` survives URL scheme changes and exercises the path a visitor takes.
- **Validate types at the source:** every price parsed to a number and every rating to 1–5, or the run fails — catch layout changes on day one.
