# Record a page once, replay it offline: HAR files with a remote browser

**Medium** · 2026-10-11 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/har-record-and-replay)

> Record every request of a page into a HAR file on your machine, then serve the page from that file — same content in a fraction of the time, nothing sent to the network, and requests the file doesn't have refused.

## The problem

Developing a scraper against a live site is slow, burns proxy traffic, and changes under you while you work. Tests that hit the real site flake. What you want is to capture the site once — through the remote browser and its proxy — and replay exactly that capture as often as you like, with no network at all.

## What we used, and why

| What | Why |
|---|---|
| `newContext({ recordHar: { path, mode: "minimal" } })` | Writes every request and response of the context into a HAR file — on your machine, even though the browser is remote. |
| `context.routeFromHAR(path, { notFound: "abort" })` | Answers requests from the HAR; anything the file doesn't contain is refused, so nothing leaks to the network. |
| `requestfinished` / `requestfailed` events | Count what the replay served from the file and what it refused. |
| books.toscrape.com | A real page with images, CSS and scripts. |

## How it works

1. Launch Chromium; open a context that records a HAR and load the page.
2. Close the context (the HAR is written) and count its entries.
3. Open a new context that answers from the HAR, load the same page, and count what was served and refused.
4. Try a page the HAR doesn't contain.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-11):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Phase | ms | Books | Detail |
|---|---|---|---|
| Live (recording) | 8902 | 20 | 31 requests → 868 KB HAR |
| Replay from HAR | 601 | 20 | 30 served, 2 refused |
| Page not in the HAR |  |  | refused |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The HAR lands on your machine,** even though the browser runs on the fleet: Playwright streams it back over the session.
- **Replay is near-instant and offline:** the page came back with the same 20 books in a fraction of the live time — no proxy traffic, no site changes between runs.
- **`notFound: "abort"` keeps replays honest:** a page that wasn't recorded is refused instead of fetched, so a test can't quietly depend on the live site.
- **Use it for development and tests,** not for fresh data: a HAR is a snapshot. Record again when the site changes.
