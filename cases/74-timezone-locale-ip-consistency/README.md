# Does the browser agree with its IP? Timezone, language and WebRTC through exits in three countries

**Medium** · 2026-10-28 · Camoufox, Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/timezone-locale-ip-consistency)

> Five setups through residential exits in the US, Germany and Japan: which ones keep timezone, language, Accept-Language, Intl formats and the WebRTC address consistent with the exit IP, and which mismatches a site can check.

## The problem

A site that knows your IP can compare it with what the browser says: is the browser's timezone the one the IP is in, does its language fit the country, does Accept-Language say the same as `navigator.language`, and does WebRTC reveal another address? Any disagreement is a cheap signal that something is off. This case measures five setups against those checks, through one sticky residential exit per country. See [what a page can read about your browser](https://cdpfleet.com/docs/fingerprinting) for the signals in general.

## What we used, and why

| What | Why |
|---|---|
| `cdpfleet-resi-country-us\|de\|jp-session-…` | One sticky residential exit per country, shared by that country's five setups. |
| `geoip` | Camoufox matches timezone, language and location to the exit IP at launch (on by default). |
| `locale` | Camoufox's language and formats, set by hand (here a deliberate mismatch, `pt-BR`). |
| `browser.newContext({ locale, timezoneId })` | For Chrome: set language and timezone per context, here from a first lookup of the exit. |
| `main_world_eval` + `"mw:"` | Reads the values in the page's own JavaScript world on Camoufox, as the site sees them. |

## How it works

1. For each country, open one sticky residential exit and run the five setups one after another on it.
2. Open the exit-lookup page (ip-api.com's JSON, which carries the exit's country, timezone and IP).
3. In that page, read `navigator.languages`, the Accept-Language the browser sends (echoed by httpbin.org), Intl's timezone and locale, a formatted number, and the WebRTC public address, compared with the exit IP and never printed.
4. List each check that disagrees with the exit.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-28):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Exit | Browser timezone | IP timezone | navigator.languages | Accept-Language | Number | WebRTC | Mismatches a site can check |
|---|---|---|---|---|---|---|---|---|
| Camoufox (geoip on, default) | US | America/New_York | America/New_York | en-US, en | en-US | 1,234,567.891 | exit IP | none |
| Camoufox, geoip: false | US | UTC | America/New_York | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone |
| Camoufox, locale: "pt-BR" | US | America/New_York | America/New_York | pt-BR, pt | pt-BR | 1.234.567,891 | exit IP | language region ≠ IP country |
| Chrome headful, no locale | US | UTC | America/New_York | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone |
| Chrome headful, context locale + timezoneId from the exit | US | America/New_York | America/New_York | en-US | en-US | 1,234,567.891 | none | none |
| Camoufox (geoip on, default) | DE | Europe/Berlin | Europe/Berlin | de-DE, de | de-DE | 1.234.567,891 | exit IP | none |
| Camoufox, geoip: false | DE | UTC | Europe/Berlin | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone; language region ≠ IP country |
| Camoufox, locale: "pt-BR" | DE | Europe/Berlin | Europe/Berlin | pt-BR, pt | pt-BR | 1.234.567,891 | exit IP | language region ≠ IP country |
| Chrome headful, no locale | DE | UTC | Europe/Berlin | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone; language region ≠ IP country |
| Chrome headful, context locale + timezoneId from the exit | DE | Europe/Berlin | Europe/Berlin | de-DE | de-DE | 1.234.567,891 | none | none |
| Camoufox (geoip on, default) | JP | Asia/Tokyo | Asia/Tokyo | ja-JP, ja | ja-JP | 1,234,567.891 | exit IP | none |
| Camoufox, geoip: false | JP | UTC | Asia/Tokyo | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone; language region ≠ IP country |
| Camoufox, locale: "pt-BR" | JP | Asia/Tokyo | Asia/Tokyo | pt-BR, pt | pt-BR | 1.234.567,891 | exit IP | language region ≠ IP country |
| Chrome headful, no locale | JP | UTC | Asia/Tokyo | en-US, en | en-US | 1,234,567.891 | none | timezone ≠ IP timezone; language region ≠ IP country |
| Chrome headful, context locale + timezoneId from the exit | JP | Asia/Tokyo | Asia/Tokyo | ja-JP | ja-JP | 1,234,567.891 | none | none |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Camoufox with `geoip` (its default) passed every check in all 15 cells across five runs.** On 2026-10-28 its timezone was the exit IP's (New York, Berlin, Tokyo), its language carried the IP's country as region, Accept-Language and Intl agreed with `navigator.language`, and WebRTC showed only the exit IP.
- **But `geoip` draws the language per launch, weighted by who lives there:** through the German exit only 2 of 5 launches picked German; the others got `en-DE`, `es-DE` and `el-DE`. The US exit got `es-US` once in five, and Japan got Japanese every time. Each is a plausible resident, but if the site expects the country's main language (a German shop, say), set `locale` yourself. Earlier, on 2026-10-22, a South African exit drew Northern Sotho.
- **Without a locale and timezone, Chrome tells on itself:** headful Chrome reported `UTC` and `en-US` through every exit in every run, so the timezone disagreed with the IP everywhere and the language region did in Germany and Japan. Setting both on the context from a first lookup of the exit (`newContext({ locale, timezoneId })`) made every check agree.
- **`geoip: false` leaves Camoufox on `UTC` and `en-US`,** the same mismatches as plain Chrome; turn it off only when you set timezone and locale yourself. **An explicit `locale` wins over `geoip` for language only:** with `locale: "pt-BR"` Camoufox kept the exit's timezone but spoke Portuguese (region BR) in all three countries.
- **Small differences remain between setups that pass:** Chrome's context locale gives a one-entry `navigator.languages` (`de-DE`) where Camoufox lists two (`de-DE, de`), and WebRTC showed no public address on Chrome but the exit IP on Camoufox; neither contradicted the IP. Formats per locale: [case 15](https://cdpfleet.com/docs/cases/three-countries); location from the exit: [case 35](https://cdpfleet.com/docs/cases/geolocation-and-permissions); Camoufox personas: [case 04](https://cdpfleet.com/docs/cases/camoufox-persona) and [case 49](https://cdpfleet.com/docs/cases/camoufox-same-device).
