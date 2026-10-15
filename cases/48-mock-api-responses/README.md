# Mock, patch and fail API responses with route.fulfill on remote browsers

**Medium** · 2026-10-15 · Chromium, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/mock-api-responses)

> Stub a backend that doesn't exist, change a real API response on its way in, simulate a 500, and drop images — all from your script with `page.route`, on Chromium, Firefox and WebKit running on the fleet.

## The problem

Testing a front end against a real backend is slow and flaky: you want to give the page exactly the data you need, check how it handles an error, or tweak one field of a real response — without a mock server. With a remote browser the question is whether request interception still works when the browser runs somewhere else.

## What we used, and why

| What | Why |
|---|---|
| `page.route(url, handler)` | Every matching request the page makes is handed to your script first — the browser is remote, the handler runs in your program. |
| `route.fulfill({ json })` | Answer with your own data; the request never reaches the network. Here the whole site, `shop.example.test`, doesn't exist. |
| `route.fetch()` + `route.fulfill({ response, json })` | Let the real request go out, change the JSON, and pass it on with the original status and headers. |
| `route.fulfill({ status: 500 })` | Simulate a failing backend to test the page's error state. |
| `route.abort()` | Drop requests you don't need, such as images, to save proxy traffic. |

## How it works

1. On each browser, route the made-up site `shop.example.test` to an HTML page served from the script, and its `/api/products` to three stubbed products.
2. Route `httpbin.org/json` through `route.fetch()` and rewrite the slideshow title; abort all image requests.
3. Open the page and read what it rendered; open it again with the API answering 500.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-15):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Stubbed products shown | Patched real response | Stub answering 500 | Images aborted | Header charset only |
|---|---|---|---|---|---|
| chromium | Red mug — $9 · Blue mug — $11 · Teapot — $24 | Patched: Sample Slide Show | error 500 | 2 | Red mug — $9 |
| firefox | Red mug — $9 · Blue mug — $11 · Teapot — $24 | Patched: Sample Slide Show | error 500 | 2 | Red mug — $9 |
| webkit | Red mug — $9 · Blue mug — $11 · Teapot — $24 | Patched: Sample Slide Show | error 500 | 2 | Red mug â€” $9 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Interception works the same on a remote browser:** on Chromium, Firefox and WebKit the page rendered the three stubbed products from a site that doesn't exist, and showed `error 500` when the stub failed. Your handlers run in your program; only the decisions travel over the connection.
- **Patching real responses:** `route.fetch()` went out through the session's proxy, and the page got the real httpbin JSON with the title changed — status and headers unchanged.
- **Declare the charset inside fulfilled HTML:** with only `text/html; charset=utf-8` in the fulfilled headers, Chromium and Firefox showed the em dash correctly but WebKit rendered `â€”` — it read the body as Latin-1. A `<meta charset="utf-8">` in the page fixed it everywhere.
- **Aborting saves traffic:** both images (one from the fake site, one real) were dropped before they were requested.
- **Routes are per page here:** `context.route` does the same for every page in the context.
