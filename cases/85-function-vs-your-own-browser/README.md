# A Function or your own browser? Screenshot, PDF and Markdown of the same pages, timed and billed

**Easy** · 2026-11-06 · Chromium, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/function-vs-your-own-browser)

> Three pages, three outputs, two ways: one POST to a Function, or launch a browser yourself, connect Playwright, capture and close. Wall time, the thread-seconds each one is billed (read back from the account's history) and what you get — through the same proxy, in five languages.

## The problem

A screenshot, a PDF or the text of one page doesn't need a browser of your own: a [Function](https://cdpfleet.com/docs/functions) is one HTTP request that returns the file. But you can do the same thing with a session — launch, connect Playwright, `goto`, capture, close — and then you control every step. Which one is faster, which costs more of your plan, and are the files the same? This case does both, for the same three pages and the same three outputs, and reads what each was billed from the account's own history.

## What we used, and why

| What | Why |
|---|---|
| `POST https://cdpfleet.com/v1/functions/<name>` | `screenshot` with `full_page: true`, `pdf` with `format: "A4"`, `markdown` with nothing extra. The call runs in a fresh tab of a pooled Camoufox browser and returns the file. |
| `proxy` in the Function body and in the launch | The same `PROXY_URL` for every call and every session, so the network path is the same on both sides. |
| `x-call-id` | Every Function response carries it; it is the call's id in the session history. |
| `GET /v1/me/sessions?from=…` | The account's history: Function calls show up as `function:<name>`, sessions as `chromium`. Billed thread-seconds = (`ended_at` − `started_at`) × `weight`. |
| `POST https://starter.cdpfleet.com/chromium/session` + Playwright `connect()` | Your own headless Chromium: launch, connect with the key header, open a 1280×720 page, capture, `browser.close()` (which ends the session). Timed from the launch request to the close. |
| `page.screenshot({fullPage: true})`, `page.pdf({format: "A4"})` | Playwright's own captures, on the session side. |
| `page.content()` and `innerText` | Playwright has no Markdown call. The session side measures the HTML you would hand to a converter (turndown, markdownify, flexmark, ReverseMarkdown, html-to-markdown) and the page's plain text — no converter is run. |

## How it works

1. For each page (example.com, books.toscrape.com, the Wikipedia article "Web scraping") and each output (full-page screenshot, A4 PDF, Markdown), call the Function and time it until the whole file has arrived.
2. Then do the same with a session of your own: launch headless Chromium, connect, open the page, capture, close; time it from the launch request.
3. Look up every call (by `x-call-id`) and every session (by `sessionId`) in `GET /v1/me/sessions` once it has ended, and compute its billed thread-seconds.
4. Describe each file: PNG dimensions, whether the PDF has fonts (a text PDF) or is a picture, the Markdown's first line versus the page's HTML and text size.
5. Finally, one session does all nine captures (three pages × three outputs) in a row, to compare with nine separate calls.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-06):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | Output | Function: time · billed · size | Function file | Own session: time · billed · size | Own session file | Measured |
|---|---|---|---|---|---|---|
| example.com | screenshot | 4.0 s · 3.8 thread-s · 81 KB | PNG 1280×800 | 1.5 s (launch 0.3 s) · 1.3 thread-s · 69 KB | PNG 1280×797 | 2026-11-06 |
| example.com | pdf | 3.1 s · 3 thread-s · 97 KB | image PDF (a 775 px tall picture) | 1.9 s (launch 0.4 s) · 1.5 thread-s · 44 KB | text PDF, 1 A4 page | 2026-11-06 |
| example.com | markdown | 4.1 s · 4.1 thread-s · 1.4 KB | Markdown | 2.1 s (launch 0.3 s) · 1.9 thread-s · 2.3 KB | HTML (innerText 1.3 KB) | 2026-11-06 |
| books.toscrape.com | screenshot | 5.2 s · 5 thread-s · 850 KB | PNG 1280×2,549 | 5.4 s (launch 0.4 s) · 5.1 thread-s · 654 KB | PNG 1280×2,549 | 2026-11-06 |
| books.toscrape.com | pdf | 7.1 s · 7 thread-s · 567 KB | image PDF (a 2,549 px tall picture) | 6.4 s (launch 0.4 s) · 6.1 thread-s · 330 KB | text PDF, 4 A4 pages | 2026-11-06 |
| books.toscrape.com | markdown | 4.0 s · 3.9 thread-s · 13 KB | Markdown | 2.0 s (launch 0.3 s) · 1.7 thread-s · 50 KB | HTML (innerText 2.0 KB) | 2026-11-06 |
| wikipedia | screenshot | 5.5 s · 5.4 thread-s · 2.9 MB | PNG 1280×10,838 | 6.9 s (launch 0.3 s) · 6.8 thread-s · 2.3 MB | PNG 1280×10,474 | 2026-11-06 |
| wikipedia | pdf | 7.6 s · 7.5 thread-s · 3.3 MB | image PDF (a 10,838 px tall picture) | 5.9 s (launch 0.5 s) · 5.6 thread-s · 263 KB | text PDF, 8 A4 pages | 2026-11-06 |
| wikipedia | markdown | 10.3 s · 10.2 thread-s · 55 KB | Markdown | 2.4 s (launch 0.4 s) · 2.2 thread-s · 409 KB | HTML (innerText 28 KB) | 2026-11-06 |
| all three | all nine | 50.9 s · 49.9 thread-s (nine calls) | — | 15.1 s · 14.8 thread-s (one session) | — | 2026-11-06 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Your own session was faster in 35 of 45 captures, launch included.** On 2026-11-06, across five runs (one per language), launching and connecting took 0.2–0.6 s (median 0.38 s). Median time per capture: screenshot 4.8 s by Function vs 3.6 s in your own session, PDF 4.4 vs 3.7 s, Markdown 4.6 vs 2.3 s (the session side only took the HTML; a converter would add its own time). Both sides swung widely with the proxy — 2.4–14.6 s for a Function call, 1.3–12.6 s for a session. The Function waits more on purpose: the [screenshot](https://cdpfleet.com/docs/functions/screenshot) waits for the network to go quiet and [markdown](https://cdpfleet.com/docs/functions/markdown) 1.5 s after `load`, where `page.goto()` stops at `load`.
- **Both are billed the same way: wall time × one thread.** Every call was in `GET /v1/me/sessions` under its `x-call-id` as `function:<name>` with weight 1, billed 0–0.45 s less than the client waited; every session was there as `chromium`, weight 1, 0.1–0.4 s under. A capture costs what it takes: the nine Function calls of a run were billed 39.4–55.3 thread-seconds, nine one-shot sessions 23.9–37.2.
- **Several outputs of the same page: one session is 2.5–4× cheaper.** One session that loaded each page once and took all three outputs from it was billed 13.2–16.5 thread-seconds for the nine captures — 2.5–4.0× less than nine Function calls and 1.8–2.8× less than nine separate sessions. Each Function call loads its page again; [scrape](https://cdpfleet.com/docs/functions/scrape) returns several formats from one load (not measured here).
- **The PDFs are different files.** The pooled Function returned an image PDF without fonts every time — 97–99 KB, 581 KB and 3.4–3.5 MB — while `page.pdf()` gave text PDFs of 45 KB (1 page), 338 KB (4 A4 pages) and 270 KB (8 pages): for the Wikipedia article 12–13× smaller, and searchable. For a text PDF from a Function, run it in [your own browser](https://cdpfleet.com/docs/functions#own-browser) (`"browser": "chrome"`, paid plans).
- **Screenshots match in width, not pixel for pixel.** Both sides were 1280 px wide in every run, and the catalogue was 2,549 px tall on both. The Function renders in Camoufox, so other pages came out at their own heights — Wikipedia 10,463 or 10,838 px against Chromium's 10,474, example.com 775–800 against 797 — and its PNGs were 17–18% (example.com) and 30% (the catalogue) larger.
- **Markdown is where the Function saves you work.** It returned the same bytes in all five runs — 1,395, 13,153 and 56,237 — each with a `# ` title and a `Source:` line. The pages' HTML was 2,373 bytes, 51,024 bytes and 318–419 KB (Wikipedia served two variants), so the Wikipedia Markdown is 13–18% of it; `innerText` was smaller still (1,301, 2,049 and 28,719 bytes) but has no links or structure. Doing it yourself means picking a converter and its rules (main content only, absolute links), which this case doesn't measure.
- **When each fits.** A Function: a one-off capture from any code that can send a POST — no Playwright, no browser to launch or close, Markdown ready for an LLM, a fresh tab and exit country per call. Your own session: several outputs or pages from one load, anything after a login or a click, text PDFs, and your own wait rules — and in this run it was also the faster and cheaper path.
