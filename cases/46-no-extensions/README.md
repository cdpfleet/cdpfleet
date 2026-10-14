# Browser extensions aren't supported — what to use instead

**Easy** · 2026-10-14 · Google Chrome, Firefox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/no-extensions)

> An honest "doesn't work" case: extension flags and lists in a launch are ignored on the fleet. Content scripts, request blocking and header rewriting — what most automation extensions are for — work with Playwright instead, on Chrome and Firefox alike.

## The problem

Locally you might load an ad blocker, a header-rewriting extension or your own content script with `--load-extension`. cdpfleet launches browsers for you, so there is no way to install an extension — and an unknown launch field doesn't raise an error, it's simply not used. What do you lose, and how do you get the same effect?

## What we used, and why

| What | Why |
|---|---|
| `args: ["--load-extension=…"]`, `extensions: […]` in the launch | What people try first. Neither is a cdpfleet launch option. |
| `chrome.runtime` / `browser.runtime` in the page | Present only when an extension exposes itself to the page. |
| `context.addInitScript()` | The content-script equivalent: runs in every page and frame before the page's own scripts, after every navigation. |
| `context.route()` + `route.abort()` | The request-blocking equivalent (ad and image blockers). |
| `context.setExtraHTTPHeaders()` | The header-rewriting equivalent, for every request the browser makes. |
| httpbin.org/headers | Echoes the request headers, to prove the header went out. |

## How it works

1. Launch Chrome and Firefox with an extension flag, a user-agent flag (as a visible marker) and an `extensions` list, and check whether either took effect.
2. In a new context, add a content script that marks the page, block images, and add a request header.
3. Open two pages of books.toscrape.com: count the pages the script marked and the images blocked; then read the headers httpbin received.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-14):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Launch with args/extensions | Took effect | Extension API in page | Content script (pages) | Images blocked | Header added |
|---|---|---|---|---|---|---|
| Chrome | 200 | no | no | 2 / 2 | 45 | yes |
| Firefox | 200 | no | no | 2 / 2 | 40 | yes |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Extensions can't be loaded:** the launch with `args` and `extensions` succeeded (200), but nothing from it reached the browser — the user-agent flag didn't change the user agent, and there was no extension API in the page. cdpfleet ignores fields it doesn't know, so check the [launch options](https://cdpfleet.com/docs/browsers/chrome) rather than relying on an error.
- **Content scripts → `addInitScript`:** the script ran on both pages, on Chrome and Firefox, and re-ran after each navigation — what a content script with `run_at: document_start` gives you.
- **Blockers → `route()`:** aborting image requests blocked 40 images across the two pages on both browsers; the same pattern blocks ad and tracker hosts ([bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet)).
- **Header rewriting → `setExtraHTTPHeaders`:** httpbin received the added header from both browsers.
- **What has no equivalent:** an extension's own UI, popup and background service worker, and extensions that call browser-internal APIs (password managers, wallets). If a site requires one of those, it won't run here.
