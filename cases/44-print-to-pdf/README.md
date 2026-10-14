# Print to PDF across browsers: which engines can, and the fallback for the rest

**Easy** · 2026-10-14 · Chromium, Google Chrome, Microsoft Edge, Firefox, Camoufox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/print-to-pdf)

> `page.pdf()` on seven remote browsers: Chromium, Chrome and Edge print — headless and headful — while Firefox, Camoufox and WebKit refuse. Print CSS plus a full-page screenshot works everywhere.

## The problem

Invoices, reports and archived pages often have to end up as PDFs. Playwright's `page.pdf()` is documented as "headless Chromium only", which raises two questions on a fleet of real browsers: does a headful Chrome or Edge print, and what do you do on Firefox, Camoufox or WebKit when the site only works there?

## What we used, and why

| What | Why |
|---|---|
| `page.setContent()` | The same test page everywhere, with a coloured background, a screen-only banner and enough text for several printed pages — no proxy traffic. |
| `page.pdf({ format: "A4" })` | Asks the browser to print the page; the PDF bytes come back over the connection. |
| `printBackground: true` | Backgrounds are left out of printed pages by default, as in a desktop print dialog. |
| `@media print` + `page.emulateMedia({ media: "print" })` | Applies the page's print stylesheet for screenshots — the part of printing every engine supports. |
| `page.screenshot({ fullPage: true })` | The fallback capture when `page.pdf()` isn't available. |

## How it works

1. Launch seven setups one after another: Chromium headless, Chrome headless and headful, Edge headful, Firefox, Camoufox and WebKit (headful, their default).
2. Load the same test page and call `page.pdf()` twice — without and with backgrounds — counting pages and bytes, or record the error.
3. Switch to print media, check that the print stylesheet hid the banner, and take a full-page screenshot.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-14):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | page.pdf | Pages | KB (with background) | Print CSS | Print screenshot KB |
|---|---|---|---|---|---|
| Chromium, headless | yes, 0.03 s | 4 | 19.6 (19.7) | applied | 595 |
| Chrome, headless | yes, 0.17 s | 4 | 19.8 (19.9) | applied | 595 |
| Chrome, headful | yes, 0.16 s | 4 | 19.8 (19.9) | applied | 595 |
| Edge, headful | yes, 0.05 s | 4 | 19.8 (19.9) | applied | 595 |
| Firefox, headful | no | — | page.pdf: PDF generation is only supported for Headless Chromium | applied | 850 |
| Camoufox, headful | no | — | page.pdf: PDF generation is only supported for Headless Chromium | applied | 312 |
| WebKit, headful | no | — | page.pdf: PDF generation is only supported for Headless Chromium | applied | 759 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Every Chromium-based browser printed — headful too:** Chrome and Edge in their default headful mode returned the same 4-page A4 PDF as headless Chromium. On the fleet, "headless only" really means "Chromium only".
- **Firefox, Camoufox and WebKit refuse** with `PDF generation is only supported for Headless Chromium` — a client-side check in Playwright, so no setting changes it.
- **Backgrounds are off by default:** `printBackground: true` gave a larger file with the page's background colour; without it the colour is dropped, like a print dialog.
- **Print CSS works everywhere:** `emulateMedia({ media: "print" })` applied the print stylesheet on all seven, so a full-page screenshot in print media is the portable fallback — an image, not selectable text.
- **Printing is fast:** both PDFs of the 4-page document came back in a fraction of a second over the remote connection.
