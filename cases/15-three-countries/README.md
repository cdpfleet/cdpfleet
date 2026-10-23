# Three countries, one script — and reading values the way the site does

**Hard** · 2026-10-01 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/three-countries)

> Camoufox personas for the US, Germany and Japan: timezone, language, local dates and numbers, geolocation, all consistent with the exit country.

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

The same program in five languages, each verified on the production fleet (last run 2026-10-23):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `PROXY_URL_US`, `PROXY_URL_JP` (see [cases/README.md](../README.md#environment)).

## What we got

| Persona | Exit | Timezone (page) | Default page.evaluate | 15:30 UTC shown as | Number | Price | Geolocation vs exit (km) |
|---|---|---|---|---|---|---|---|
| United States | Lafayette, United States | America/New_York | America/New_York | 10/1/2026, 11:30:00 AM | 1,234,567.891 | €49.90 | 1580 |
| Germany | Munich, Germany | Europe/Berlin | Europe/Berlin | 1.10.2026, 17:30:00 | 1.234.567,891 | 49,90 € | 501 |
| Japan | Kobe, Japan | Asia/Tokyo | Asia/Tokyo | 2026/10/2 0:30:00 | 1,234,567.891 | €49.90 | 425 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Timezone and languages are consistent:** the browser timezone matches the exit country, `navigator.languages` and `Accept-Language` follow `locale`, and the same instant (15:30 UTC) shows as local time — morning in New York, afternoon in Berlin, after midnight in Tokyo.
- **`locale` reaches JavaScript's formats too.** With `locale: "de-DE"` the German persona formats `1.234.567,891` and `1.10.2026, 17:30:00`, and the Japanese one `2026/10/2 0:30:00`, matching their languages. Measured 2026-10-23 on the fleet's current Camoufox 156 build (the official Camoufox library); the first 156 build, live 2026-10-21 to 2026-10-22, kept `Intl` on `en-US` whatever the locale.
- **The old trap is gone on 156:** on 152 a default `page.evaluate` reported `UTC` for all three (Playwright's isolated world wasn't patched); on 156 it reports the persona's timezone too. `main_world_eval` with the `"mw:"` prefix is still the way to read exactly what the site's scripts see.
- **Geolocation lands in the right country, not the right city:** geo-IP databases disagree, and a rotating residential proxy may exit from a different IP than the one Camoufox looked up at launch. Use sticky sessions when city-level consistency matters.
- **Currency is the site's choice, not the browser's:** a euro price is only formatted, never converted. It's `€49.90` for the US and Japanese personas and `49,90 €` for the German one.
