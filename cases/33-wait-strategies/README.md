# When is a page ready? commit, domcontentloaded, load, networkidle or a selector

**Easy** · 2026-10-10 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/wait-strategies)

> Five ways to wait for a page, timed on a static catalogue, a big article and a page that renders its data late — and how many data items were actually there when each wait returned.

## The problem

`page.goto()` waits for `load` by default, scripts often add `networkidle` "to be safe", and both feel like "the page is ready". Through a proxy every extra wait costs seconds — and on pages that fetch or render data after load, none of them means the data is there. Which wait gets you the data soonest, and which ones lie?

## What we used, and why

| What | Why |
|---|---|
| `waitUntil: "commit"` | Returns as soon as the response starts — nothing rendered yet. |
| `waitUntil: "domcontentloaded"` | The HTML is parsed; images, scripts started later and data loads may still be running. |
| `waitUntil: "load"` | Every subresource has loaded (the default). One slow image or ad sets the pace. |
| `waitUntil: "networkidle"` | No requests for 500 ms. Discouraged by Playwright itself — and not a data signal. |
| `locator(data).first().waitFor()` after `commit` | Wait for the thing you came for. |
| A fresh context per measurement | No cache between runs, so every strategy waits for the same work. |

## How it works

1. Launch one Chromium session.
2. For each page and each strategy: open a new context, navigate with that wait, time it, and count the data items present right after.
3. Print every timing with its item count.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-10):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | Wait | ms | Data items present |
|---|---|---|---|
| https://books.toscrape.com/ | commit | 6475 | 4 |
| https://books.toscrape.com/ | domcontentloaded | 3491 | 20 |
| https://books.toscrape.com/ | load | 3513 | 20 |
| https://books.toscrape.com/ | networkidle | 2563 | 20 |
| https://books.toscrape.com/ | selector | 3145 | 17 |
| https://en.wikipedia.org/wiki/Web_scraping | commit | 2634 | 35 |
| https://en.wikipedia.org/wiki/Web_scraping | domcontentloaded | 8730 | 35 |
| https://en.wikipedia.org/wiki/Web_scraping | load | 6203 | 35 |
| https://en.wikipedia.org/wiki/Web_scraping | networkidle | 5573 | 35 |
| https://en.wikipedia.org/wiki/Web_scraping | selector | 5110 | 35 |
| https://quotes.toscrape.com/js-delayed/ | commit | 1163 | 0 |
| https://quotes.toscrape.com/js-delayed/ | domcontentloaded | 2691 | 0 |
| https://quotes.toscrape.com/js-delayed/ | load | 6802 | 0 |
| https://quotes.toscrape.com/js-delayed/ | networkidle | 11819 | 0 |
| https://quotes.toscrape.com/js-delayed/ | selector | 13731 | 10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Only a wait for the data returned with the data:** on the page that renders its quotes with a delayed script, `commit`, `domcontentloaded`, `load` and `networkidle` all returned with **zero** quotes in all five languages; the selector wait got all 10.
- **`networkidle` is not "data ready"** — it returned after 4–7 s on that page with nothing on it. It also inherits every slow request: 3.5–49 s on the Wikipedia article across runs.
- **`.first().waitFor()` means "at least one":** on the catalogue it sometimes returned with 4 or 12 of the 20 books. When you need them all, wait for the count (`expect(locator).toHaveCount(20)`) or a "done" marker.
- **Through a proxy, timings swing by 10×** between runs (`load` took 1.9–33 s on the catalogue) — waits tied to all network activity are the least predictable. `commit` is too early (Wikipedia's paragraphs were missing in 4 of 5 runs); `domcontentloaded` plus a selector wait is the reliable pair.
