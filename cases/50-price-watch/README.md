# A price watcher in one program: two snapshots minutes apart, diffed, billed only while reading

**Easy** · 2026-10-15 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/price-watch)

> Take a snapshot of a catalogue price and a live market price, wait two minutes, take another and diff them field by field. Each snapshot is its own short session, so a 2-minute watch costs seconds of browser time — and an idle session would have ended anyway.

## The problem

Price and stock monitoring means reading the same pages again and again and reporting what changed. The tempting version keeps one browser open and re-reads the page every few minutes — which bills the whole time, and on cdpfleet ends after the 60-second idle timeout anyway. How do you structure a watcher so it pays only for the reading, and how do you tell a real change from noise?

## What we used, and why

| What | Why |
|---|---|
| One session per snapshot | Launch, read every target, `browser.close()`. Between snapshots nothing runs and nothing is billed. |
| `page.evaluate(read)` per target | Each target has its own small read function: the price and stock text of a product page, the amount from a JSON price API. |
| A field-by-field diff | Compare the two snapshots per field and add the percentage change for numeric prices. |
| `WAIT_SECONDS = 120` | Stands in for your scheduler's interval (cron, a queue) so the case can be verified in one run. |

## How it works

1. Snapshot 1: one short session reads a product page on books.toscrape.com and the Bitcoin spot price from Coinbase's public API.
2. Wait two minutes with no session open.
3. Snapshot 2 the same way, then diff the two and report what changed.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-15):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Target | Changed? | Before | After |
|---|---|---|---|
| Book price | no | price: £51.77; stock: In stock (22 available) | same |
| Bitcoin spot (USD) | yes (+0.05%) | price: 81676.54 | price: 81715.885 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Pay for the reading, not the waiting:** two short sessions billed about 15 seconds of browser time for a watch that took more than two minutes. Keeping one browser open would bill every second — and with no traffic it would end after the 60-second `inactivity_timeout`.
- **Static and live targets behave as expected:** the book's price and stock text were identical in both snapshots; in our run the market price moved between them, and the diff reports the before and after values with the percentage.
- **Diff fields, not pages:** comparing whole pages flags every rotating ad and timestamp. Read the fields you care about, compare those, and alert only on them.
- **In production, store the last snapshot** (a file or a database row) and run the same program from your scheduler; the diff logic doesn't change. For many URLs, read them in one session per snapshot as here — or in parallel sessions within your threads.
