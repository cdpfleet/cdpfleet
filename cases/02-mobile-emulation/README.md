# Emulating an iPhone on real Chrome: what changes, and what gives you away

**Medium** · 2026-09-30 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/mobile-emulation)

> Playwright device descriptors fix the viewport, touch and user agent — and leave five tells a site can read.

## The problem

You need the mobile version of a site, so you open a context with Playwright's `iPhone 15 Pro` or `Pixel 7` descriptor. The page renders the mobile layout. But is the browser now convincing as a phone? A bot check can look at the network (TLS, client hints) and at JavaScript APIs the descriptor doesn't touch. Let's compare a desktop context, an iPhone context and a Pixel context in the **same** Chrome session.

## What we used, and why

| What | Why |
|---|---|
| `chrome` | Real Google Chrome, the most common desktop browser. One session serves all three contexts. |
| `devices['iPhone 15 Pro']`, `devices['Pixel 7']` | Playwright's descriptors: user agent, viewport, screen, device scale factor, `isMobile`, `hasTouch`. (Playwright for Java has no registry, so the Java version spells the same values out.) |
| tls.peet.ws | Shows what the network sees: the user agent, client-hint headers and TLS fingerprint. |
| `page.evaluate` | Reads what page scripts see: viewport, touch points, pointer type, `navigator.platform`, `navigator.userAgentData`. |

## How it works

1. Launch one Chrome session.
2. Open three contexts: plain, iPhone 15 Pro, Pixel 7.
3. In each, load tls.peet.ws and read the JavaScript-visible device signals.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Context | User agent | Viewport | DPR | Touch | navigator.platform | userAgentData | JA4 |
|---|---|---|---|---|---|---|---|
| desktop | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/203.0.113.1 Safari/537.36 | 1280x720 | 1 | 0 | Linux x86_64 | Linux | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| iPhone 15 Pro | Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1 | 980x1644 | 3 | 1 | Linux x86_64 | iOS | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| Pixel 7 | Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.7778.96 Mobile Safari/537.36 | 981x1996 | 2.625 | 1 | Linux x86_64 | Android | t13d1517h2_8daaf6152771_cb7bf5808d99 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **What changes:** the user agent, viewport, screen size, device pixel ratio, touch points, `pointer: coarse`, and the `sec-ch-ua-mobile` / `sec-ch-ua-platform` client hints.
- **Tell 1 — TLS:** the JA4 and HTTP/2 fingerprints are Chrome's in all three contexts. An "iPhone Safari" with Chrome's TLS is exactly what fingerprinting vendors look for.
- **Tell 2 — client hints on "Safari":** Safari never sends `sec-ch-ua-*` headers or exposes `navigator.userAgentData`; the emulated iPhone does both (and says `platform: iOS`).
- **Tell 3 — `navigator.platform` stays `Linux x86_64`** in both mobile contexts. A real iPhone says `iPhone`, a real Pixel `Linux armv81`.
- **Tell 4 — version skew on Android:** the Pixel descriptor's user agent says Chrome 148 while the browser (and its client hints) is Chrome 153.
- **Tell 5 — headless:** the desktop context's user agent says `HeadlessChrome`. Use `headless: false` (2 threads) when that matters, or set a user agent.
- Emulation is fine for layout and responsive testing. To look like a real phone to a bot check, use a real device — cdpfleet's Android and iPhone sessions are in early access.
