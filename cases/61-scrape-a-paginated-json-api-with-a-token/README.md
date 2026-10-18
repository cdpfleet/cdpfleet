# Borrow the page's token and read its paginated API directly

**Medium** · 2026-10-18 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/scrape-a-paginated-json-api-with-a-token)

> Log in once in a remote browser, find the bearer token the app sends to its own API, and fetch every page of the API from inside the page: the same records as clicking through every page, with 16 fields each instead of 5 columns, in about a second instead of many.

## The problem

Many sites behind a login are single-page apps: the table you see is drawn from a JSON API, and the app proves who you are with a token on every call. Scraping the table means clicking through pages and parsing cells; the API has the same records with every field, but answers 401 without the token. The token is already in the browser after you log in — the question is where to find it, how long it lasts, and how to use it so the requests still come from the browser. The site here is Toolshop (practicesoftwaretesting.com), a public demo shop built for practising test automation, with its published demo account.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session. The login, the token and the API calls all live in it. |
| `page.setDefaultTimeout(90000)` | Through a rotating residential exit the app sometimes took 30 s to draw its login form — longer than Playwright's default 30 s for an action. |
| `localStorage.getItem('auth-token')` | Where this app keeps its token after login (a JWT). Other apps use a cookie, `sessionStorage` or memory only. |
| `page.waitForRequest(…)` + `allHeaders()` | Catches the app's own API call and reads its `Authorization` header — this works even when the token is never stored anywhere you can read. |
| `page.evaluate(() => fetch(url, { headers: { authorization } }))` | Calls the API from inside the page, so the requests come from the browser, through the session's proxy, with the site's own origin — not from a different HTTP stack ([four ways to make a request](https://cdpfleet.com/docs/cases/proxy-paths)). |

## How it works

1. Open the login page and sign in with the site's demo account.
2. Read the token from `localStorage`, open the invoices page and catch the app's first `/invoices` request to compare its `Authorization` header; decode the JWT's lifetime.
3. Click Next through all pages, reading the table each time, and count the app's `/invoices` calls.
4. Fetch every page of `/invoices?page=N` from inside the page with the token: one by one, then all at once; then once without the token.
5. Compare the records both ways collected.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-18):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Way | Invoices | /invoices calls | Data per invoice | Time (s) |
|---|---|---|---|---|
| Click Next through the table | 162 | 11 | 5 columns | 5.5 |
| API with the token, one by one | 162 | 11 | 16 fields | 4.2 |
| API with the token, all at once | 162 | 11 | 16 fields | 0.8 |
| API without the token | 0 | 1 | HTTP 401 |  |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The token is in the browser after login:** the app stored a JWT under `auth-token` in localStorage and sent exactly that as `Authorization: Bearer …` on its own API calls. Without it the API answered **401**.
- **Same records, more data:** the API returned every invoice the table showed, about 160 of them. The page showed 5 columns; the API returned **16 fields** per invoice, including the invoice lines and payment — about 400 KB of JSON.
- **Page numbers over a list that changes:** the demo account is shared with everyone practising on the site, and invoices were added while the programs ran: while building this case, one run counted 161 in the table and 162 from the API, another got 164 entries from 11 API pages for 163 invoices: a new invoice at the top pushed one onto the next page, so it was read twice. De-duplicate by id when you page by number, and expect to miss what moves the other way.
- **The page size is the server's:** 15 per page — 11 or 12 pages as the list grew — and `per_page` or `limit` parameters were ignored, so the API needs as many requests as the clicks do: one `/invoices` call per page either way.
- **The win is parallelism, not skipping the UI:** fetched one by one, the 11 API pages took about as long as clicking Next through the pages — several seconds, each request a round trip through the residential exit — but **all at once they took about a second**. Without a token you could only click.
- **Tokens expire:** this one lived **300 seconds** (`exp − iat` in the JWT). Check `exp` before a long job and log in again — or read the fresh token the page keeps — when it runs out.
- Compare with [infinite scroll, two ways](https://cdpfleet.com/docs/cases/infinite-scroll), where the page's API needs no token, and [fetch first](https://cdpfleet.com/docs/cases/fetch-first), which skips the browser when the HTML already has the data.
