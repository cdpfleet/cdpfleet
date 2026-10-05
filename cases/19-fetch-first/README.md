# Fetch first, browser second: paying for JavaScript only when the page needs it

**Easy** · 2026-10-05 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/fetch-first)

> A plain HTTP GET through your proxy tells you in a second whether the data is in the HTML. Only the pages that render with JavaScript get a browser session — three pages, one decision rule, measured.

## The problem

A browser is the expensive way to download HTML. Many pages — catalogues, listings, documentation — ship their data in the HTML and need no JavaScript at all; others are empty shells filled in by scripts, and some need scrolling or clicks on top. If you send every URL to a browser you pay thread-seconds for pages `curl` could have fetched; if you send none you silently get empty results from the JavaScript ones. The question is how to decide per page, cheaply, through the same proxy, without maintaining two code paths.

## What we used, and why

| What | Why |
|---|---|
| `playwright.request.newContext({ proxy })` | Playwright's HTTP client, run locally with your proxy — no session, no thread, same exit IP as the browser would have. Available in all five languages. |
| A browser-like `User-Agent` on the plain request | So the server answers the plain GET the way it answers the browser; otherwise some sites serve a different page to a bare client. |
| Counting the item markup in the raw HTML | The decision rule: if the HTML already contains the expected items (`class="product_pod"`, `class="quote"`), there is nothing a browser would add. |
| `chromium`, `headless: "new"`, launched lazily | One session for all the pages that failed the rule; none at all if every page passed. |
| `locator(…).first().waitFor()` then `count()` | In the browser, wait for the rendered items rather than for "load": JavaScript-rendered pages fire `load` before their content exists. |

## How it works

1. GET each page through the proxy with the plain client; record status, size, how many items the HTML contains and the time.
2. Pages whose HTML has fewer items than expected are marked as needing a browser.
3. Launch one headless Chromium session only if that list is non-empty; open each such page, wait for the items to render, count them, record the time.
4. Print one row per page: what the HTML had, what the browser had, which path it needed.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-05):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | Status | HTML (KB) | Items in HTML | Items in browser | Needed a browser | Fetch (s) | Browser (s) |
|---|---|---|---|---|---|---|---|
| https://books.toscrape.com/ | 200 | 50 | 20 | — | false | 3.55 | — |
| https://quotes.toscrape.com/js/ | 200 | 6 | 0 | 10 | true | 1.223 | 11.043 |
| https://quotes.toscrape.com/scroll | 200 | 3 | 0 | 10 | true | 2.605 | 6.12 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One of the three pages needed no browser:** the bookshop's HTML contained all 20 products, so the plain GET was the whole job — no session, no thread-seconds.
- **The two JavaScript pages had zero items in their HTML** (6 KB and 3 KB shells) and 10 each in the browser; the rule caught both and the browser was launched once for the pair.
- **The plain fetch is the cheap probe:** a second or so through the proxy, against several seconds per page for a navigation that waits for rendering — and nothing billed until the first browser launch.
- **Keep the probe and the browser on the same proxy:** the server then sees one visitor, not a bare HTTP client from one IP followed by a browser from another.
- **Expected counts beat "is there any content":** a shell page is not empty — it has a header, a footer and 6 KB of markup. Compare against what a rendered page would contain.
