# Geo compare: the same pages from five countries in one call, and reading the diff

**Easy** · 2026-11-08 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/geo-compare)

> One `POST /v1/functions/geo` per page loads it from the US, Germany, the UK, Japan and Brazil at once and returns each country's Markdown and HTML plus a diff against the first. A Steam store page, Google's home page, Nike's home page and example.com: prices and currency, language, cookie banners, country redirects — and what to check before you trust the diff. In five languages.

## The problem

Prices, currency, language, cookie banners and even which page you get depend on where the visitor is. Checking that usually means one browser or proxy per country, the same page loaded in each, and a diff tool at the end. The [geo compare Function](https://cdpfleet.com/docs/functions/geo) does all of it in one HTTP request: it opens the page in up to five countries at once, each in its own tab with that country's exit IP, locale, timezone and `Accept-Language`, and returns every country's result plus a unified diff of each text format against the first country. This case runs it on four pages and reads what came back — including what the diff doesn't tell you.

## What we used, and why

| What | Why |
|---|---|
| `POST https://cdpfleet.com/v1/functions/geo` | `countries: ["us", "de", "gb", "jp", "br"]` (2–5, the first is the baseline), `formats: ["markdown", "html"]` (any [scrape](https://cdpfleet.com/docs/functions/scrape) formats), `max_wait: 45`. No `proxy`: each country runs through the Community proxy in that country. |
| `results.<country>` | `{status, partial?, markdown, html}` for a country that loaded, `{status: null, error}` for one that didn't. There is no final URL per country (the [scrape](https://cdpfleet.com/docs/functions/scrape) Function has `url`; geo doesn't), so the programs read `<link rel="canonical">` from the HTML. |
| `diff.markdown.<country>`, `diff.html.<country>` | A unified diff (`--- us`, `+++ de`, `@@` hunks) of each text format against the first country. The programs count its hunks and added/removed lines and pair the prices on removed and added lines (`$9.99 → £8.19`). |
| The HTML | `<html lang>` for the language and an "accept all" button label (`Accept all`, `Alle akzeptieren`, `Aceitar todos`, `すべて同意`, …) for a cookie banner; prices in the Markdown by currency symbol. |
| `x-call-id` + `GET /v1/me/sessions` | The call's record in the account's history: engine `function:geo`, its weight (one thread per country) and the thread-seconds billed. |
| One retry | When the baseline country failed, the call is made once more: every diff is against it, and then every diff comes back `""`. |

## How it works

1. For each page (a Steam store page, Google's home page, Nike's home page, example.com as the control), one geo call with the five countries — one page after another.
2. If the first country (`us`) didn't load, call once more and keep what the first attempt said.
3. Per country: status or error, `lang`, canonical URL, title, prices and their currencies, whether a cookie banner is in the HTML, sizes, and whether the Markdown is identical to the baseline's.
4. Per diff: bytes, hunks, lines added and removed, and the first price changes; per call: the time, the response size and how much of it is diff.
5. Look each call up in the session history by its `x-call-id`: engine, weight and billed thread-seconds.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-08):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | Country | Result | lang | Canonical URL | Prices | Cookie banner | Markdown diff vs us | Call | Measured |
|---|---|---|---|---|---|---|---|---|---|
| Steam store page | us | 200 | en | https://store.steampowered.com/app/620/Portal_2/ | $5.69, $1.99 (USD) | no | baseline | 46.4 s · 230.7 thread-s · 2.7 MB, diffs 154 KB Markdown + 1.2 MB HTML | 2026-11-08 |
| Steam store page | de | 200 (partial) | de | https://store.steampowered.com/app/620/Portal_2/ | 9,69€, 1,59€ (EUR) | yes | 47 KB: +214 −208 lines; $5.69 → 9,69€, $1.99 → 1,59€, $11.38 → 19,38€ | — | 2026-11-08 |
| Steam store page | gb | page_timeout | — | — | — | — | "" (failed) | — | 2026-11-08 |
| Steam store page | jp | 200 | ja | https://store.steampowered.com/app/620/Portal_2/ | ¥ 1,400, ¥ 205 (JPY) | no | 45 KB: +168 −168 lines; $5.69 → ¥ 1,400, $1.99 → ¥ 205, $11.38 → ¥ 2,800 | — | 2026-11-08 |
| Steam store page | br | 200 (partial) | pt-br | https://store.steampowered.com/app/620/Portal_2/ | R$ 33,99, R$ 4,49 (BRL) | no | 62 KB: +618 −335 lines; $5.69 → R$ 33,99, $1.99 → R$ 4,49, $11.38 → R$ 67,98 | — | 2026-11-08 |
| Google home page | us | 200 | en | — | — | no | baseline | 35.9 s · 178.8 thread-s · 3.5 MB, diffs 23 KB Markdown + 2.0 MB HTML | 2026-11-08 |
| Google home page | de | 200 | de | — | — | yes | 8.9 KB: +140 −17 lines | — | 2026-11-08 |
| Google home page | gb | 200 | en-GB | — | — | yes | 7.2 KB: +137 −13 lines | — | 2026-11-08 |
| Google home page | jp | 200 | ja | — | — | no | 3.5 KB: +16 −18 lines | — | 2026-11-08 |
| Google home page | br | 200 | pt-BR | — | — | no | 3.4 KB: +15 −21 lines | — | 2026-11-08 |
| Nike home page | us | 200 (partial) | en-US | https://www.nike.com/ | — | no | baseline | 36 s · 177.4 thread-s · 18.5 MB, diffs 294 KB Markdown + 10.5 MB HTML | 2026-11-08 |
| Nike home page | de | 200 (partial) | de-DE | https://www.nike.com/de/ | 50 €, £50 (EUR, GBP) | no | 69 KB: +657 −1,121 lines | — | 2026-11-08 |
| Nike home page | gb | 200 | en-GB | https://www.nike.com/gb/ | £50, £100 (GBP) | yes | 70 KB: +683 −1,102 lines | — | 2026-11-08 |
| Nike home page | jp | 200 | ja-JP | https://www.nike.com/jp/ | — | no | 81 KB: +797 −1,087 lines | — | 2026-11-08 |
| Nike home page | br | 200 | en-US | https://www.nike.com/ | — | no | 75 KB: +804 −1,081 lines | — | 2026-11-08 |
| example.com (control) | us | 200 | en | — | — | no | baseline | 8.1 s · 40 thread-s · 19 KB, diffs 0 bytes Markdown + 0 bytes HTML | 2026-11-08 |
| example.com (control) | de | 200 | en | — | — | no | "" (identical) | — | 2026-11-08 |
| example.com (control) | gb | 200 | en | — | — | no | "" (identical) | — | 2026-11-08 |
| example.com (control) | jp | 200 | en | — | — | no | "" (identical) | — | 2026-11-08 |
| example.com (control) | br | 200 | en | — | — | no | "" (identical) | — | 2026-11-08 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One call, five countries, one bill per country.** On 2026-11-08, across five runs (one per language), every call returned 200 with a result for each country and was in the history as `function:geo` with weight 5: billed 5 × its wall time, never more. A call takes as long as its slowest country — example.com 5.7–16.9 s, Google 10.7–35.9 s, Nike 32.1–40.9 s, Steam 27.9–46.8 s — so the four pages cost 430–628 thread-seconds a run, 7–10.5 thread-minutes.
- **The control is clean.** example.com came back byte-identical from all five countries in every run, and every diff was `""`: what the diff shows on the other pages comes from the sites, not from the Function.
- **Prices and currency: the diff hands you the changes.** Steam's page priced Portal 2 at 9,69€, £8.19, ¥ 1,400 and R$ 33,99, in German, Japanese and Brazilian Portuguese, and the Markdown diff's removed and added lines paired them directly (`$9.99 → £8.19`, `$1.99 → ¥ 205`). The US price itself was $9.99 in two runs and $5.69 in two: Steam prices more than one region in dollars, and in one run the US exit got no price at all, but a cookie banner.
- **When the language changes, the diff is the whole page.** Where both the US and the UK page loaded in their own currency, the UK diff (English, dates as "18 Apr, 2011") was 2.7 and 3.4 KB in 6 and 9 hunks with every price change in it; the German, Japanese and Portuguese pages gave diffs of 44–64 KB in 1–4 hunks, because every line is translated. Read prices from each country's Markdown, and use the diff where the language stays the same.
- **Cookie banners follow where the site places you, not the country you asked for.** Steam showed its "Accept All / Reject All" popup on every page priced in euros or pounds and on none priced in dollars, yen or reais — it is in the HTML, not in the Markdown. Google's "Before you continue" dialog appeared exactly when its `lang` put the visitor in the EU or UK (`de`, `en-GB`, `en-EE`) and about tripled the Markdown (up to 6,927 bytes from Germany and 5,585–6,239 from the UK, against 2,008–2,030 from the US); in one run Google placed the German exit in Iraq (`de-IQ`) and showed none, in another it answered the German exit with **429** (blocked). Nike showed its "Data improves your experience" consent from the UK in every run and from Germany in three of five.
- **Country sites have no final URL in the result — read the canonical link.** geo returns `status` and the formats, not `url`. Nike's `<link rel="canonical">` was `/de/`, `/gb/` and `/jp/` for those countries in every run, and the global `www.nike.com/` (in English) for the US and Brazil. A currency regex finds what is on the page, not what you pay: Nike's German page also carried "£50" next to "50 €".
- **Check `status` before you read a diff.** Three of the 100 country loads ended in `page_timeout` (Steam from the UK and Japan): the call was still 200, the country came back as `{status: null, error: "page_timeout"}` — and its diff entry was `""`, the same string an identical page gets. When the baseline itself fails, every diff is `""`, which is why the programs retry once in that case.
- **Ask only for the formats you need.** With `html`, Nike's response was 19.4–20.6 MB, 11.0–11.9 MB of it HTML diffs, against 276–301 KB of Markdown diffs; over the four pages the HTML diffs were 30–36× the Markdown ones, and mostly markup. `markdown` (or `text`) gives a diff you can read; add `html` only for what Markdown drops — `lang`, the canonical link, a banner.
