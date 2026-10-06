# Check every link on a page: 73 links in seconds, from inside the browser

**Easy** · 2026-10-06 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/link-checker)

> A link checker that stays the browser: collect the links, verify them with fetch() inside the page eight at a time, and compare with navigating to each one.

## The problem

Checking that every link on a page still works is the oldest crawl job there is, and the obvious browser version — open the page, click or navigate to each link, read the status — is also the slowest: every navigation loads a whole page with its stylesheets, scripts and images through your proxy. Doing the checks from your own HTTP client instead is fast, but then the checks come from a different IP, a different TLS stack and a different user agent than the browsing session ([four ways to make a request](https://cdpfleet.com/docs/cases/proxy-paths)). The middle path is to let the browser do the requests without navigating.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session, one thread; nothing here is browser-specific. |
| `page.$$eval("a[href]", …)` | Collects every link as an absolute URL, same-site only, de-duplicated, fragments dropped. |
| `fetch(url, { cache: "no-store" })` inside `page.evaluate`, 8 URLs per call | The browser's network stack, proxy, cookies and headers — but no navigation, no rendering, no sub-resources. One round trip per wave of eight. |
| `page.goto` for a 10-link sample | The baseline: what a user-like check costs per link. |
| `page.on("request")` counter | Shows what the navigations really fetched: not 10 pages but 288 requests. |

## How it works

1. Open the front page and collect its same-site links (73 unique URLs).
2. Check all 73 with `fetch()` inside the page, eight at a time; record status, redirects and failures.
3. Navigate to the first 10 links one by one with `page.goto`, counting every request the browser makes.
4. Print one row per method: links checked, OK / redirected / broken, seconds, seconds per link, requests made.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-06):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Links | OK | Redirected | Broken | Seconds | Per link (s) | Requests |
|---|---|---|---|---|---|---|---|
| fetch() in the page, 8 at a time | 73 | 73 | 0 | 0 | 7.629 | 0.1 | 73 |
| page.goto each link (10-link sample) | 10 | 10 | 0 | 0 | 22.248 | 2.22 | 288 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **In-page fetch checked 73 links in 4–13 seconds — 0.06–0.17 s per link — through a residential proxy** (one slow-proxy run took 54 s). Navigating cost 1.3–2.2 s per link in the same runs: 12–25× more, because each page load pulled in its assets (288 requests for 10 pages).
- **The checks are indistinguishable from the browser browsing:** same exit, same TLS fingerprint, same cookies and `Accept-Language`, because they *are* the browser. Your own HTTP client would have been faster still, and a different visitor.
- **Status, not content:** `fetch` gives you the status code and whether the request was redirected, which is all a link check needs. If you need the page's rendered state, that is a navigation, and it costs what navigations cost.
- **Waves, not one-by-one:** eight concurrent fetches per `page.evaluate` call means one proxy round trip per eight links. Raise the wave size until the site or the proxy pushes back.
- **All 73 links were fine** — the useful output of a link checker is usually an empty list.
