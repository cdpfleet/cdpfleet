# A page for an LLM: HTML, innerText or the accessibility snapshot?

**Easy** · 2026-10-10 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/page-to-text-for-llms)

> Three ways to hand a rendered page to a model — raw HTML, innerText and Playwright's ARIA snapshot — measured on three real pages: how big each is and what it keeps.

## The problem

Agents and extraction prompts need the page as text. Raw HTML is the obvious choice and the most expensive one; `innerText` is small but throws away links and structure. Playwright can also give you the page as the accessibility tree sees it — roles, names and links in YAML. How big is each on real pages, and what does the model get?

## What we used, and why

| What | Why |
|---|---|
| `page.content()` | The serialized DOM — everything, markup included. |
| `locator("body").innerText()` | What a person reads: text only, no links or structure. |
| `locator("body").ariaSnapshot()` | The accessibility tree as YAML: headings, lists, buttons, links with their URLs — the structure a screen reader uses. |
| Three page shapes | Hacker News (a table layout), a Wikipedia article (long prose) and a shop catalogue. |

## How it works

1. Launch Chromium and open each page (waiting for the HTML).
2. Take the HTML, the inner text and the ARIA snapshot of the body.
3. Compare their sizes, count the links the snapshot keeps, and print the first lines of each snapshot.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-10):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | HTML chars | innerText chars | ARIA snapshot chars | Snapshot / HTML % | Links in snapshot |
|---|---|---|---|---|---|
| https://news.ycombinator.com/ | 34742 | 4096 | 40476 | 116.5 | 224 |
| https://en.wikipedia.org/wiki/Web_scraping | 256505 | 28589 | 71320 | 27.8 | 383 |
| https://books.toscrape.com/ | 51004 | 2029 | 16662 | 32.7 | 84 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The ARIA snapshot is a third of the HTML on ordinary pages** (about 28% on Wikipedia, 33% on the catalogue) while keeping every link with its URL, headings and lists.
- **On table layouts it can be bigger than the HTML:** Hacker News' nested tables repeat each row's text as row and cell names — 117% of the HTML. Snapshot a smaller locator (the story list) instead of `body`.
- **`innerText` is the smallest by far** (5–12% of the HTML) — fine for summaries, useless when the model has to click or follow a link.
- **Snapshot the part you need:** `page.locator("main").ariaSnapshot()` or a list locator cuts tokens more than any format choice.
