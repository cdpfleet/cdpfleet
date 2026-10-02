# A German Windows persona with Camoufox — and the one setting that broke it

**Hard** · 2026-09-30 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/camoufox-persona)

> OS, locale, screen, window, WebRTC and geo-IP in one consistent fingerprint. The proxy decides whether the story holds.

## The problem

Camoufox (a hardened Firefox) generates a whole device fingerprint from a few options. The goal: a German user on a Windows desktop — German language headers, a Windows user agent and platform, a common screen size, no WebRTC leak, and a timezone that matches where the IP is. We run the same persona twice: through a random residential exit, and through one in Germany.

## What we used, and why

| What | Why |
|---|---|
| `os: "windows"` | Windows user agent, `navigator.platform: Win32`, Windows-typical fonts and GPU strings. |
| `locale: "de-DE"` | `navigator.languages` and the `Accept-Language` header in German. |
| `screen`, `window` | Pin a common 1920×1080 screen and a 1600×900 window instead of random sizes. |
| `humanize: true` | Human-like cursor movement for anything you click. |
| `block_webrtc: true` | No `RTCPeerConnection`, so WebRTC can't leak the real network path. |
| `geoip: true` | Timezone and geolocation follow the proxy's exit IP. This is the option that makes or breaks the persona. |
| `PROXY_URL_DE` | The same residential provider with a German exit (most providers take a country in the username, e.g. `-country-de`). |

## How it works

1. Launch Camoufox with the persona through a random exit, read what the page and the network see, then look up the exit IP's country.
2. Do the same through a German exit.
3. Compare: do language, timezone and exit country tell the same story?

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE` (see [cases/README.md](../README.md#environment)).

## What we got

| Exit | IP country | Browser timezone | Accept-Language | Platform | Screen | Window | WebRTC |
|---|---|---|---|---|---|---|---|
| random exit | United States | Asia/Kuala_Lumpur | de-DE,de;q=0.9 | Win32 | 1920x1080 | 1600x900 | false |
| German exit | Germany | Europe/Berlin | de-DE,de;q=0.9 | Win32 | 1920x1080 | 1600x900 | false |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Everything Camoufox controls came out as asked** in both runs: Windows user agent and platform, `de-DE` languages and header, 1920×1080 screen, 1600×900 window, no WebRTC, a plausible Windows GPU.
- **The random exit broke the story:** a German-speaking Windows user in whatever timezone the random exit happened to be in (see the table). `geoip` did its job — it matched the IP — but the IP didn't match the persona. **Choose the proxy country to match the locale.**
- **Rotating residential proxies can split the story further:** the timezone is set from the exit IP at launch, and a rotating proxy may exit somewhere else a moment later — in some of our runs the browser timezone and the IP a website saw were in different countries. Use sticky sessions (same IP for the whole session) with `geoip`.
- **The German exit is consistent end to end:** `de-DE`, `Europe/Berlin`, German IP.
- Fingerprint values that Camoufox randomizes (GPU, core count) differ between sessions — that's intended: each session is a different "device".
