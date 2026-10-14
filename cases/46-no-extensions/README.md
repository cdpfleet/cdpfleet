# Browser extensions aren't supported — what to use instead

**Easy** · 2026-10-14 · Google Chrome, Firefox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/no-extensions)

> An honest "doesn't work" case: a launch with extension flags or an extension list is refused (`400 unsupported_option`). Content scripts, request blocking and header rewriting — what most automation extensions are for — work with Playwright instead, on Chrome and Firefox alike.

## The problem

Locally you might load an ad blocker, a header-rewriting extension or your own content script with `--load-extension`. cdpfleet launches browsers for you, so there is no way to install an extension, and launch fields that would change the browser process are refused. What do you lose, and how do you get the same effect?

## What we used, and why

| What | Why |
|---|---|
| `args: ["--load-extension=…"]`, `extensions: […]` in the launch | What people try first. Both are browser-process fields, which the fleet refuses with `400 unsupported_option`, as it does `executablePath`, `ignoreDefaultArgs`, `env`, `userDataDir`, `chromiumSandbox`, `devtools`, `downloadsPath` and `tracesDir`. |
| `chrome.runtime` / `browser.runtime` in the page | Present only when an extension exposes itself to the page. |
| `context.addInitScript()` | The content-script equivalent: runs in every page and frame before the page's own scripts, after every navigation. |
| `context.route()` + `route.abort()` | The request-blocking equivalent (ad and image blockers). |
| `context.setExtraHTTPHeaders()` | The header-rewriting equivalent, for every request the browser makes. |
| httpbin.org/headers | Echoes the request headers, to prove the header went out. |

## How it works

1. Ask for Chrome and Firefox with an extension flag, a user-agent flag and an `extensions` list, and record the answer.
2. Launch both normally and check the page for an extension API.
3. In a new context, add a content script that marks the page, block images, and add a request header.
4. Open two pages of books.toscrape.com: count the pages the script marked and the images blocked; then read the headers httpbin received.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-14):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Launch with args/extensions | Extension API in page | Content script (pages) | Images blocked | Header added |
|---|---|---|---|---|---|
| Chrome | 400 unsupported_option | no | 2 / 2 | 44 | yes |
| Firefox | 400 unsupported_option | no | 2 / 2 | 40 | yes |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Extensions can't be loaded, and you're told so:** the launch with `args` and `extensions` was refused with `400 {"error":"unsupported_option"}` on both browsers, so nothing starts and nothing is billed; a normal launch has no extension API in the page. The same goes for the other browser-process fields above — use the [launch options](https://cdpfleet.com/docs/browsers/chrome) instead.
- **Content scripts → `addInitScript`:** the script ran on both pages, on Chrome and Firefox, and re-ran after each navigation — what a content script with `run_at: document_start` gives you.
- **Blockers → `route()`:** aborting image requests blocked every image on the two pages (40 or more) on both browsers; the same pattern blocks ad and tracker hosts ([bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet)).
- **Header rewriting → `setExtraHTTPHeaders`:** httpbin received the added header from both browsers.
- **What has no equivalent:** an extension's own UI, popup and background service worker, and extensions that call browser-internal APIs (password managers, wallets). If a site requires one of those, it won't run here.
