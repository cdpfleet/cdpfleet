# A German IP with a UTC clock: making location, timezone and permissions agree

**Easy** · 2026-10-11 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/geolocation-and-permissions)

> What a page learns about where you are — geolocation, its permission, timezone and language — through a German proxy, with defaults and with a Berlin context. Defaults contradict the IP; three context options fix it.

## The problem

A proxy changes where your traffic comes from, not what the browser says about itself. Sites compare the IP's country with the browser's timezone, language and — if they ask — its geolocation. A German IP with a UTC clock, `en-US` and a refused location prompt is an easy inconsistency to flag. What does the browser report by default, and what does it take to make it agree with the proxy?

## What we used, and why

| What | Why |
|---|---|
| A German proxy | The exit the site sees; ip-api.com confirms its country and timezone. |
| `navigator.geolocation` and `navigator.permissions` | What a page reads when it asks where you are. |
| `Intl.DateTimeFormat().resolvedOptions().timeZone`, `navigator.language` | The clock and language the browser reports. |
| `newContext({ permissions: ["geolocation"], geolocation, timezoneId, locale })` | Per-context overrides for all of them — no relaunch. |

## How it works

1. Launch Chromium through a German proxy and look up the exit country and timezone.
2. In a default context, read geolocation, its permission, timezone and language.
3. Repeat with the geolocation permission granted but no position set.
4. Repeat with Berlin coordinates, `Europe/Berlin` and `de-DE`.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-11):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE` (see [cases/README.md](../README.md#environment)).

## What we got

| Context | Geolocation | Permission | Timezone | Language |
|---|---|---|---|---|
| defaults | permission denied | prompt | UTC | en-US |
| permission granted, no position | Timeout expired | granted | UTC | en-US |
| Berlin: geolocation + timezone + locale | 52.52, 13.41 | granted | Europe/Berlin | de-DE |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Defaults contradict the proxy:** the exit was German (`Europe/Berlin`) but the browser said UTC and `en-US`, and refused geolocation.
- **Granting the permission isn't enough:** without a position, `getCurrentPosition` just times out — the page sees a permission it can't use.
- **Three options make it consistent:** `geolocation`, `timezoneId` and `locale` on the context, plus the `geolocation` permission — the page then sees Berlin, `Europe/Berlin` (UTC+2) and `de-DE`.
- **Match them to the exit, per context:** look up your proxy's country first (or use a country-pinned [residential token](https://cdpfleet.com/docs/proxies#residential)); Camoufox can do this for you with `geoip: true`.
