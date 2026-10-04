# What your browser says before it says anything: TLS and HTTP/2 fingerprints

**Medium** · 2026-09-30 · Google Chrome, Microsoft Edge, Firefox, Camoufox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/tls-fingerprints)

> Five engines, one endpoint (tls.peet.ws), and the fingerprints anti-bot systems read before your page even loads.

## The problem

Anti-bot systems don't wait for JavaScript. The first thing a server sees is the TLS ClientHello (cipher suites, extensions, their order) and, on HTTP/2, the SETTINGS and WINDOW_UPDATE frames. Those are condensed into fingerprints — **JA3/JA4** for TLS, the **Akamai HTTP/2 fingerprint** for h2 — and compared with what the user agent claims to be. A Chrome user agent over a Python-requests TLS stack is flagged instantly. So: what does each cdpfleet engine actually send?

## What we used, and why

| What | Why |
|---|---|
| `chrome`, `edge`, `firefox`, `camoufox`, `webkit` | One of each engine family, to compare Chromium, Gecko and WebKit network stacks. |
| `proxy` | Every session needs one; fingerprints are made by the browser, not the proxy, so any proxy works. |
| `headless: true` | TLS and HTTP/2 don't change between headless and headful — the cheapest mode (1 thread) is enough. |
| `page.goto(tls.peet.ws/api/all)` | The browser itself makes the request. `page.request` / `APIRequest` would go out through Playwright's own HTTP client, with a different fingerprint. |

## How it works

1. Launch each browser with a proxy.
2. Navigate to `https://tls.peet.ws/api/all` and read the JSON the page returns.
3. Keep the user agent, HTTP version, JA4, JA3 hash, peetprint and Akamai HTTP/2 fingerprint.
4. Close the browser — that ends the session and the billing.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-04):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Version | HTTP | JA4 | Akamai h2 hash | Ciphers | Extensions |
|---|---|---|---|---|---|---|
| chrome | 154.0.8037.97 | h2 | t13d1517h2_8daaf6152771_cb7bf5808d99 | 52d84b11737d980aef856699f885ca86 | 16 | 19 |
| edge | 154.0.4258.53 | h2 | t13d1516h2_8daaf6152771_806a8c22fdea | 52d84b11737d980aef856699f885ca86 | 16 | 18 |
| firefox | 150.0.2 | h2 | t13d1617h2_86a278354501_3cbfd9057e0d | 6ea73faa8fc5aac76bded7bd238f6433 | 16 | 17 |
| camoufox | 152.0.4-beta.30 | h2 | t13d1617h2_86a278354501_3cbfd9057e0d | 6ea73faa8fc5aac76bded7bd238f6433 | 16 | 17 |
| webkit | 26.4 | HTTP/1.1 | t13d2913h1_723694b0fccc_5671b5df5029 | — | 29 | 13 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Each engine sends its own real fingerprint.** Chrome's JA4 and HTTP/2 settings are Chrome's; Firefox's are Firefox's. Nothing to patch.
- **Chrome and Edge differ by one extension** (JA4 `…1517…` vs `…1516…`) but share the HTTP/2 fingerprint — both are Chromium network stacks.
- **Camoufox picks a random OS for its user agent in every session** (set `os` to choose one) **— and whatever it claims, its TLS is identical to Linux Firefox.** That's fine: Firefox's TLS stack (NSS) is the same on every OS, so a real Windows or Mac Firefox sends exactly this.
- **WebKit speaks HTTP/1.1 through a proxy, with 29 cipher suites** — nothing like Safari on a Mac (HTTP/2, fewer ciphers). Use WebKit for rendering tests, not to impersonate Safari.
- Fingerprints are stable per build: read them once per version, not per request.
