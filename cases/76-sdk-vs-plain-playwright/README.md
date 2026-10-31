# The SDK vs plain Playwright: the same scrape, and what happens at the thread cap

**Easy** · 2026-10-29 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/sdk-vs-plain-playwright)

> One small scrape written three ways in each language: a launch request of your own plus Playwright, the cdpfleet SDK, and the SDK's connect URL. The scrape is the same either way; the difference shows when the account is out of threads, where the SDK waits for a free one and the plain request gets an immediate 429.

## The problem

cdpfleet works with plain Playwright: one HTTP request launches a browser, `connect()` with the key header drives it. The [SDKs](https://cdpfleet.com/docs/sdk) for Node, Python and Go wrap that. Is there a reason to add a dependency for a two-step API? This case runs the same scrape both ways, measures it, and then fills a Community account's two threads to see what each does when a launch is refused.

## What we used, and why

| What | Why |
|---|---|
| Plain launch | `POST https://starter.cdpfleet.com/chromium/session` with `x-api-key`, then Playwright's `connect(wsUrl)` with the same header; `browser.close()` ends the session. |
| SDK `launch()` + `connect()` | `Cdpfleet().launch('chromium', options)` returns a session; `session.connect(chromium)` adds the key header; `session.close()` cleans up. On `429` (except daily or hourly quota) and `503` it waits `Retry-After` and tries again, for up to 120 s by default. |
| SDK `connectUrl()` | A launch-on-connect URL: one Playwright `connect()` launches the browser and connects to it, no launch request in your code. |
| A Community account | Two threads, so holding two launched sessions reaches the cap. Community launches are limited to 2 a second and 12 a minute, so the program spaces them by 1.5 s. |
| `DELETE /v1/me/threads/:id` | Frees a thread 5 s into the SDK's wait, and ends every session at the end. |
| C# and Java | No published SDK yet (Node, Python and Go are on npm, PyPI and the Go proxy), so these programs show the plain version and write the retry loop by hand. |

## How it works

1. Scrape three catalogue pages of books.toscrape.com (60 titles) with a plain launch request and Playwright; record when the browser was connected and the total time.
2. Do the same with the SDK's `launch()` and `connect()`, then with its connect URL.
3. On the Community account, launch two sessions and keep them, then send a third launch request and record the answer.
4. Launch again with the SDK (or the hand-written loop) while a timer frees one thread after 5 s; record how long the launch took to return a session.
5. End every session and check that no thread is left running.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-29):

- [Node.js](node.mjs) — npm install playwright@1.60.0 cdpfleet@0.1.1 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests cdpfleet==0.1.1 && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 github.com/cdpfleet/cdpfleet/sdk/go@v0.1.1 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `COMMUNITY_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Language | Plain Playwright | SDK launch() + connect() | Connect URL | Third launch at the cap | Launch that waits | Measured |
|---|---|---|---|---|---|---|
| node | 60 books · connected in 579 ms · 8.4 s | 60 books · connected in 370 ms · 11.2 s | 60 books · connected in 328 ms · 6.8 s | 429 threads_exceeded, Retry-After 1 s | SDK: session after 6.5 s | 2026-10-29 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The scrape itself doesn't change.** All 15 variants on 2026-10-29 (plain, SDK and connect URL in Node, Python and Go; plain and connect URL in Java and C#) loaded the same 60 books. A browser was connected 0.3–0.7 s after the launch started in every variant but one (the first launch of the C# run, 2.0 s). The total time, 3.8 to 34.5 s, followed the proxy's page loads from run to run, not the way the browser was launched.
- **The difference is at the cap.** With both Community threads in use, a plain launch request got `429 threads_exceeded` at once, with `Retry-After: 1`, `in_use: 2` and an `upgrade` object linking to [/upgrade](https://cdpfleet.com/upgrade). Your own code has to decide what to do with that. The SDK's `launch()` kept trying as `Retry-After` asked and returned a session 5.5–6.9 s after it started, within about 2 s of the thread being freed at 5 s.
- **That retry is what you write yourself without an SDK.** In Java and C# the same behaviour took a 14- and 15-line loop: retry `503` and `429`, except the daily and hourly quota (they don't clear in seconds), wait `Retry-After` or back off, and give up after 120 s. It returned in 6.5–7.0 s, like the SDKs. The SDKs also add the key header when connecting, give you the session id, and end sessions on `close()`.
- **A launched session holds a thread from the launch on: until it is closed, or, if nobody connects, for its 60-second connect window.** The sessions held to reach the cap were never connected, and neither was the one the waiting launch returned, so each program ended them at once with `DELETE /v1/me/threads/:id` (`session.end()` in the SDKs). Every run finished with no thread left running.
- **Which to use:** plain Playwright is two steps and works in every language, which is fine for scripts that run one browser at a time. Use an SDK (Node, Python, Go) or the same retry loop when several workers share an account's threads and a refused launch should wait rather than fail. The connect URL removes the launch request altogether: see [/docs/connect](https://cdpfleet.com/docs/connect) and [/docs/sdk](https://cdpfleet.com/docs/sdk).
