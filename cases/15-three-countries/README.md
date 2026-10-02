# Three countries, one script — and reading values the way the site does

**Hard** · 2026-10-01 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/three-countries)

> Camoufox personas for the US, Germany and Japan: timezone, language, local dates and numbers, geolocation. Plus the Playwright trap that reports UTC for all of them.

## The problem

Prices, search results and availability change by country, so you run the same flow as a visitor from several countries. Each visit has to be consistent — IP, timezone, language and formats from the same country — or the site notices. And when you check that from your script, you have to read the values the way the site does.

## What we used, and why

| What | Why |
|---|---|
| `PROXY_URL_US`, `_DE`, `_JP` | Exits in each country (most providers take a country in the proxy username). |
| `locale` | Per country: `en-US`, `de-DE`, `ja-JP` — languages, `Accept-Language`, and number/date formats. |
| `geoip: true` | Camoufox sets the timezone and geolocation from the proxy's exit IP at launch. |
| `main_world_eval: true` + `"mw:"` | Runs the check in the page's own JavaScript world — the world the site's scripts see. |
| `context.grantPermissions(['geolocation'])` | As if the visitor allowed location access. |

## How it works

1. Launch one Camoufox session per country with that country's exit and locale, all at once.
2. Look up the exit IP's city, timezone and coordinates.
3. In the page's main world, read languages, timezone, a fixed UTC instant formatted locally, a number, a price and the geolocation.
4. For contrast, read the timezone again with a default `page.evaluate`.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-01):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `PROXY_URL_US`, `PROXY_URL_JP` (see [cases/README.md](../README.md#environment)).

## What we got

| Persona | Exit | Timezone (page) | Default page.evaluate | 15:30 UTC shown as | Number | Price | Geolocation vs exit (km) |
|---|---|---|---|---|---|---|---|
| United States | Birmingham, United States | America/New_York | UTC | 10/1/2026, 11:30:00 AM | 1,234,567.891 | €49.90 | 573 |
| Germany | Berlin, Germany | Europe/Berlin | UTC | 1.10.2026, 17:30:00 | 1.234.567,891 | 49,90 € | 446 |
| Japan | Nagoya, Japan | Asia/Tokyo | UTC | 2026/10/2 0:30:00 | 1,234,567.891 | €49.90 | 138 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Each persona is consistent:** the browser timezone matches the exit country, and the same instant (15:30 UTC) shows as local time — morning in New York, afternoon in Berlin, after midnight in Tokyo — with local number formats.
- **The trap: a default `page.evaluate` in Camoufox reported `UTC` for all three.** Camoufox runs Playwright's scripts in an isolated world that isn't patched like the page's own world. The site saw the right timezone from the first millisecond; only the check was wrong. Read fingerprint values with `main_world_eval` and the `"mw:"` prefix.
- **Geolocation lands in the right country, not the right city:** geo-IP databases disagree, and a rotating residential proxy may exit from a different IP than the one Camoufox looked up at launch. Use sticky sessions when city-level consistency matters.
- **Currency is the site's choice, not the browser's:** a price in euros is formatted as `€49.90` or `49,90 €` depending on the locale — the browser never converts it.
