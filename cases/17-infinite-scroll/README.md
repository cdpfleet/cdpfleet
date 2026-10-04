# Infinite scroll, two ways: scroll like a user, or call the JSON the page calls

**Easy** · 2026-10-04 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/infinite-scroll)

> Collecting everything an endlessly scrolling page loads: by scrolling and watching the DOM, and by catching the page's own API request and paging through it from inside the browser.

## The problem

Infinite-scroll pages have no "next" link and no end: content appears as you scroll, and the only way to know you've got everything is that scrolling stops producing more. The naive script scrolls, waits, counts, repeats — and has to guess how long to wait. But the page gets its data from somewhere: an XHR to a JSON endpoint, usually paginated, usually unauthenticated. Which approach is faster, which is more reliable, and how do you call that endpoint without losing the browser's proxy, cookies and TLS fingerprint?

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | Nothing here depends on the browser; the cheapest session (1 thread) does. |
| `page.on("request")` | Counts every request each method causes — page assets included — so the two can be compared. |
| `window.scrollTo(0, document.body.scrollHeight)` in a loop | The user-like way: scroll, wait, count `.quote` elements, stop after three scrolls that added nothing. |
| `page.waitForResponse(xhr)` | Catches the first JSON request the page makes for its own content, which gives us the endpoint, its parameters and page 1 of the data. |
| `page.evaluate(urls => Promise.all(urls.map(fetch)))` | Calls the endpoint from inside the page: the browser's proxy, cookies, headers and TLS fingerprint, four pages per round trip. |
| A fresh tab per method | So request counts and timings don't mix. |

## How it works

1. Open the page and wait for the first quotes to render; record the load time separately from the collection time.
2. **Scroll:** scroll to the bottom, wait 600 ms, count the quotes; stop when three scrolls in a row add nothing; read the quotes from the DOM.
3. **API:** in a new tab, wait for the page's first XHR to `/api/…`, take page 1 from its body, then fetch pages 2–5, 6–9, 10–13 in waves of four with `fetch` inside the page until a wave reports no next page.
4. Print, per method: quotes and distinct authors found, scrolls or API pages, requests made, load and collection seconds.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-04):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Quotes | Authors | Scrolls | API pages | Requests | Load (s) | Collect (s) |
|---|---|---|---|---|---|---|---|
| scroll the page | 100 | 50 | 12 | — | 16 | 5.761 | 8.785 |
| call its JSON API | 100 | 50 | — | 13 | 19 | 4.635 | 1.405 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Both methods find the same 100 quotes by 50 authors**, so the JSON route loses nothing — and gains structure: the API returns authors as objects with slugs and tags as arrays, which the DOM route would have to parse.
- **Collecting through the API was faster** once the page had loaded — three round trips through the proxy for twelve pages instead of a run of scroll-and-wait cycles (5 to 15 across our runs: the faster the proxy, the more pages one cycle happens to catch). Through a residential proxy every round trip costs real time, so fetch pages in waves, not one by one: our first attempt, one request at a time, was slower than scrolling.
- **Scrolling needs a stopping rule, and the rule costs time or correctness:** "three scrolls that added nothing" means three waits after the real end on every run — and on a slow proxy moment it can still stop early, as one of our C# runs did at 10 quotes. The API tells you (`has_next: false`).
- **Fetching from inside the page keeps you indistinguishable from the page:** the proxy, the cookies, the `Accept`/`Origin` headers and the TLS fingerprint are the browser's. A fetch from your script's own HTTP client would have none of them.
- **The last wave overshoots** (pages 11–13 came back empty) — a cheap price for parallelism; size the wave to the latency you see.
