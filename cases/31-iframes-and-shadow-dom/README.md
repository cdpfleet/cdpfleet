# Content you can't querySelector: iframes and shadow DOM

**Medium** · 2026-10-10 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/iframes-and-shadow-dom)

> Same-origin and cross-origin iframes, open and nested shadow roots, and a closed one: what document.querySelector finds (nothing), what Playwright locators find (all but the closed root), and how.

## The problem

Modern pages hide content where a plain `document.querySelector` can't reach: widgets in iframes (often from another origin — payments, maps, chat), and design-system components that render inside shadow roots. A scraper that evaluates `querySelector` in the page comes back empty and looks broken. Which of these can you reach from Playwright, and with what?

## What we used, and why

| What | Why |
|---|---|
| `page.setContent()` with a test page | One page with every case — a `srcdoc` iframe, a cross-origin iframe (httpbin.org), an open shadow root with a nested one, and a closed shadow root — so the result doesn't depend on a third-party layout. |
| `document.querySelector` in `evaluate` | The baseline: what page JavaScript sees from the top document. |
| `page.frameLocator(sel).locator(…)` | Reaches into an iframe, same-origin or not — the remote browser runs out-of-process frames for you. |
| `page.locator(css)` | Playwright's CSS engine pierces open shadow roots at any depth. |
| `page.frames()` | Every frame as an object with its own URL and title. |

## How it works

1. Launch Chromium and load the test page; wait until the cross-origin frame has rendered its heading.
2. Look for each target with `document.querySelector` from the top document.
3. Look for the same targets with Playwright locators and frame locators.
4. Print what each approach found, and the frames the page has.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-10):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Target | document.querySelector | Playwright | How |
|---|---|---|---|
| same-origin iframe | — (not found) | same-origin iframe text | page.frameLocator('#same').locator('#inner') |
| cross-origin iframe | — (not found) | Herman Melville - Moby-Dick | page.frameLocator('#cross').locator('h1') |
| open shadow root | — (not found) | open shadow text | page.locator('.msg') — CSS pierces open shadow roots |
| nested open shadow root | — (not found) | nested shadow text | page.locator('.badge') — any depth |
| closed shadow root | — (not found) | — (not found) | not reachable from page scripts or locators |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **`querySelector` from the top document finds none of it** — not the iframes' content, not even the open shadow roots.
- **Frame locators reach both iframes,** including the cross-origin one: Playwright talks to each frame's own process, which page JavaScript can't.
- **Locators pierce open shadow roots at any depth** with plain CSS — no `>>>`, no `shadowRoot` walking.
- **Closed shadow roots stay closed:** neither page scripts nor locators can read them. Use what they render (screenshots, accessibility tree) or the component's public API.
- **Every language uses the same calls** (`frameLocator`, `locator`) — the five programs print identical results.
