# Setting a header the wrong way: header order as a fingerprint

**Hard** · 2026-09-30 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/headers-and-h2)

> Four ways to change request headers in Playwright, and what each does to the header order a server sees. TLS and HTTP/2 stay put; the order doesn't.

## The problem

You need German content, or a tracing header on every request. Playwright gives you `locale`, `extraHTTPHeaders` and `route.continue({ headers })`. They all "work" — but browsers send headers in a fixed order, and some bot checks compare that order with the browser's real one. Which methods keep Chrome looking like Chrome?

## What we used, and why

| What | Why |
|---|---|
| `chrome`, `headless: false` | Real Chrome on a virtual display: the reference header order. |
| `locale: "de-DE"` | A context option; sets `navigator.language` and the `Accept-Language` header. |
| `extraHTTPHeaders` | Adds or overrides headers on every request of a context. |
| `page.route` + `route.continue({ headers })` | Rewrites each request's headers in flight. |
| tls.peet.ws | Echoes the HTTP/2 HEADERS frame exactly as received — names in order — plus the TLS and Akamai HTTP/2 fingerprints. |

## How it works

1. Launch one Chrome session.
2. For each method, open a fresh context, apply it and load tls.peet.ws.
3. Compare the header order, the `Accept-Language` value and the fingerprints.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Accept-Language | Header order (after the pseudo-headers) | JA4 |
|---|---|---|---|
| default | en-US,en;q=0.9 | pragma, cache-control, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform, upgrade-insecure-requests, user-agent, accept, sec-fetch-site, sec-fetch-mode, sec-fetch-user, sec-fetch-dest, accept-encoding, accept-language, priority | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| locale: de-DE | de-DE | pragma, cache-control, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform, upgrade-insecure-requests, user-agent, accept-language, accept, sec-fetch-site, sec-fetch-mode, sec-fetch-user, sec-fetch-dest, accept-encoding, priority | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| extraHTTPHeaders | de-DE | pragma, cache-control, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform, upgrade-insecure-requests, user-agent, accept-language, x-request-id, accept, sec-fetch-site, sec-fetch-mode, sec-fetch-user, sec-fetch-dest, accept-encoding, priority | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| route: rewrite headers | en-US,en;q=0.9 | pragma, cache-control, accept, upgrade-insecure-requests, user-agent, x-request-id, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform, sec-fetch-site, sec-fetch-mode, sec-fetch-user, sec-fetch-dest, accept-encoding, accept-language, priority | t13d1517h2_8daaf6152771_cb7bf5808d99 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **TLS (JA4) and the Akamai HTTP/2 fingerprint never change** — they come from the network stack, not from headers.
- **Chrome's own order puts `accept-language` near the end, after `accept-encoding`.** Both `locale` and `extraHTTPHeaders` move it up, right after `user-agent` — an order real Chrome doesn't send on a navigation.
- **`extraHTTPHeaders` also sends exactly what you wrote:** `de-DE` instead of Chrome's own `de-DE,de;q=0.9`.
- **`route.continue({ headers })` rewrites the order entirely:** `accept` jumps up, client hints move after `user-agent`, custom headers land in the middle. It's the most visible of all.
- **Best:** don't touch headers on navigations if a site checks them. For the language, prefer a launch option the browser applies itself (e.g. Camoufox's `locale`, see the Camoufox persona case) — and check the result against tls.peet.ws the same way as here.
