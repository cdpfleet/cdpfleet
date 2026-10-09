# Visual capture: full-page screenshot, element crop and PDF in one session

**Easy** · 2026-10-09 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/screenshot-and-pdf)

> Four captures from one page load: a viewport screenshot, a full-page screenshot, a single-element crop and a PDF — all generated in the remote browser and streamed back as files.

## The problem

You need visual proof of what a page looks like — for monitoring, archiving, testing or reporting. A viewport screenshot captures what fits in the window, but pages scroll. A full-page screenshot stitches the entire document. An element screenshot isolates one component. A PDF gives you a printable, searchable document. Each serves a different purpose, and all four can come from the same session without reloading the page.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | PDF generation requires headless Chromium. |
| `viewport: { width: 1280, height: 720 }` | A fixed viewport makes viewport screenshots reproducible across runs. |
| `page.screenshot({ fullPage: false })` | The visible area only — 1280x720 pixels, smallest file. |
| `page.screenshot({ fullPage: true })` | Stitches the entire scrollable page into one tall image. |
| `locator('.product_pod').first().screenshot()` | Crops to the bounding box of a single element — useful for component-level visual testing. |
| `page.pdf()` | Renders the page as a PDF document; only available in headless Chromium. |
| `waitUntil: "networkidle"` | Ensures all images and styles have loaded before capturing. |

## How it works

1. Open books.toscrape.com with a 1280x720 viewport and wait for network idle.
2. Take a viewport screenshot (visible area only) and measure the file size.
3. Take a full-page screenshot (entire scrollable page) and measure the file size.
4. Take an element screenshot of the first product card and measure the file size.
5. Generate a PDF of the page and measure the file size.
6. Print one JSON object with each capture's type, file size and timing.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-09):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Capture | File size | Seconds |
|---|---|---|
| Viewport screenshot (1280x720) | 150 KB | 0.74 |
| Full-page screenshot | 403 KB | 1.12 |
| Element screenshot (first product) | 15 KB | 0.31 |
| PDF | 81 KB | 0.85 |

Full output: [output.json](output.json).

## Takeaways

- **Four capture types, one page load:** the page is already rendered in the remote browser; each capture is a different serialisation of the same state — no re-navigation, no extra proxy traffic.
- **Full-page screenshots are 2–3x larger than viewport screenshots** because they stitch the entire scrollable area. Budget accordingly for storage and transfer.
- **Element screenshots are tiny** — useful for visual regression testing when you only care about one component, not the whole page.
- **PDF requires headless Chromium** — it will not work in headed mode or with Firefox/WebKit. The PDF is searchable and printable, unlike a screenshot.
- **Files are generated in the remote browser and streamed back** over the fleet connection. They never touch the proxy; the capture is renderer-to-client.
