# A bandwidth diet for residential proxies: what blocking really saves

**Medium** · 2026-09-30 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/bandwidth-diet)

> Count every byte a news homepage pulls through your proxy, then block images, media, fonts and third parties — and see where the money actually goes.

## The problem

Residential proxies bill per gigabyte, so a scraper's cost is mostly bytes. The usual advice is "block images". How much does that save on a real, ad-heavy page — and what else is worth blocking?

## What we used, and why

| What | Why |
|---|---|
| `context.route('**/*')` + `route.abort()` | Drops requests before they leave the browser, so they never cost proxy bandwidth. |
| `request.resourceType()` | Classifies requests: `image`, `media`, `font`, `script`, `xhr`, … |
| A first-party list | The site's own domains and CDNs (`bbc.com`, `bbc.co.uk`, `bbci.co.uk`); everything else is third-party (ads, analytics, trackers). |
| `request.sizes()` on `requestfinished` | Exact bytes on the wire per request: request and response headers plus the encoded body. |
| A fresh context per variant | Empty cache each time, so every variant pays full price. |

## How it works

1. Load the page with nothing blocked; count requests and bytes for DOM-ready plus 5 seconds.
2. Again, blocking images, media and fonts.
3. Again, also blocking every third-party host.
4. Convert to proxy cost per 100,000 page loads at $3/GB.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Variant | Requests | Blocked | KB | Saved | Proxy cost / 100k pages | DOM ready (ms) |
|---|---|---|---|---|---|---|
| everything | 80 | 0 | 1662 | 0% | $476 | 4314 |
| no images, media or fonts | 84 | 28 | 1533 | 8% | $439 | 5030 |
| …and first-party only | 53 | 31 | 791 | 52% | $226 | 2688 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Images, media and fonts are a smaller share than you'd think** on a script-heavy news page — at most about a fifth, and on a single load it can even look worse: the ad slots fill differently every time.
- **Third parties are the bigger lever:** ads, analytics and consent tooling pull scripts, pixels and beacons from dozens of hosts. Keeping only first-party requests cut the bytes much further.
- **Blocking can also make pages faster** — fewer requests competing for the proxy. But test it: some sites need a third-party script (consent, anti-bot) to render at all.
- `load` may never fire when a video is blocked; measure with `domcontentloaded` plus a fixed window, as here.
- Numbers move between runs (ad auctions differ per load); the proportions hold.
