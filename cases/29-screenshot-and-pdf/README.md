# Visual capture: full-page screenshot, element crop and PDF in one session

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/screenshot-and-pdf)

> Four captures from one page load — viewport, full page, a single element and a PDF — rendered in the remote browser and saved on your machine, with their sizes and timings.

## The problem

You need visual proof of what a page showed — for monitoring, archiving, tests or a report. A viewport screenshot only covers the window; pages scroll; sometimes you want one component; sometimes a document. All of it can come from the one page the remote browser already rendered — but how big and how slow is each, once the bytes have to travel back to you?

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | `page.pdf()` needs headless Chromium. |
| `page.screenshot()` | The visible area of the viewport. |
| `page.screenshot({ fullPage: true })` | The whole scrollable page in one tall image. |
| `locator(".product_pod").first().screenshot()` | Crops to one element's box — for component-level checks. |
| `page.pdf()` | The page as a print document. |
| `waitUntil: "networkidle"` | Images and styles have loaded before anything is captured. |

## How it works

1. Open books.toscrape.com and wait for the network to go quiet.
2. Take a viewport screenshot, a full-page screenshot and an element screenshot of the first product card, saving each locally.
3. Render the page as a PDF.
4. Print the size and time of every capture.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Capture | KB | Seconds |
|---|---|---|
| viewport screenshot | 139 | 0.18 |
| full-page screenshot | 654 | 0.54 |
| element screenshot | 23 | 0.16 |
| pdf | 329 | 0.18 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Four captures, one page load:** each is a different serialisation of the same rendered state — no re-navigation, no extra proxy traffic.
- **The bytes travel back over the session's connection:** a full-page PNG is several times a viewport one, so prefer element crops for repeated visual checks.
- **PDF is headless Chromium only** — on Firefox, WebKit or a headful session use screenshots instead.
- **Wait for the network first:** capturing before images arrive gives you grey boxes that look like a broken page.
