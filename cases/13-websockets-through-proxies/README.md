# Real-time sites through a proxy: WebSocket latency by proxy

**Medium** · 2026-10-01 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/websockets-through-proxies)

> Chats, live prices and dashboards run on WebSockets. They work through every proxy type — but each message pays the full proxy path, and location decides how much.

## The problem

Live sites push data over WebSockets, and your browser's WebSocket goes through your proxy like everything else. Does it work through residential and SOCKS proxies, and how much latency does each add to every message?

## What we used, and why

| What | Why |
|---|---|
| Three sessions, three proxies | A random residential exit, a residential exit in Germany, and a datacenter SOCKS5 proxy. |
| `wss://ws.postman-echo.com/raw` | A public echo server: every message comes straight back. |
| `page.evaluate` with `WebSocket` | The browser opens the socket itself (through the proxy), times the handshake and 20 sequential round trips. |
| ip-api.com | Where each proxy exits, to explain the numbers. |

## How it works

1. For each proxy, launch a session and look up its exit.
2. From a real https page, open the WebSocket and time the connection.
3. Send 20 messages one at a time and record each round trip; report the median and 95th percentile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-01):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `SOCKS_PROXIES` (see [cases/README.md](../README.md#environment)).

## What we got

| Proxy | Exit | Connect (ms) | Round trip p50 (ms) | p95 (ms) |
|---|---|---|---|---|
| residential (any country) | Ho Chi Minh City, Vietnam | 676 | 185 | 196 |
| residential (Germany) | Hagen, Germany | 575 | 142 | 147 |
| datacenter SOCKS5 | Los Angeles, United States | 662 | 110 | 120 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **WebSockets work through every proxy type** — residential and SOCKS5 alike; they ride the same CONNECT tunnel as HTTPS.
- **Every message pays the whole path:** browser (our servers, in Germany) → proxy exit → the site → back. The round-trip time is the sum of both legs.
- **Location beats proxy type:** a proxy exit close to both our servers and the site wins; a residential exit on another continent adds hundreds of milliseconds to every message.
- For latency-sensitive sites, choose the exit country near the site's servers, and measure — residential pools vary from exit to exit.
