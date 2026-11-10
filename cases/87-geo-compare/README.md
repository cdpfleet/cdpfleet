# Geo compare: the same pages from five countries in one call, and reading the diff

**Easy** · 2026-11-08 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/geo-compare)

> One `POST /v1/functions/geo` per page loads it from the US, Germany, the UK, Japan and Brazil at once and returns each country's Markdown and HTML plus a diff against the first. A Steam store page, Google's home page, Nike's home page and example.com: prices and currency, language, cookie banners, country redirects — and what to check before you trust the diff. In five languages.

## The problem

Prices, currency, language, cookie banners and even which page you get depend on where the visitor is. Checking that usually means one browser or proxy per country, the same page loaded in each, and a diff tool at the end. The [geo compare Function](https://cdpfleet.com/docs/functions/geo) does all of it in one HTTP request: it opens the page in up to five countries at once, each in its own tab with that country's exit IP, locale, timezone and `Accept-Language`, and returns every country's result plus a unified diff of each text format against the first country. This case runs it on four pages and reads what came back — including what the diff doesn't tell you.

## What we used, and why

| What | Why |
|---|---|
| `POST https://cdpfleet.com/v1/functions/geo` | `countries: ["us", "de", "gb", "jp", "br"]` (2–5, the first is the baseline), `formats: ["markdown", "html"]` (any [scrape](https://cdpfleet.com/docs/functions/scrape) formats), `max_wait: 45`. No `proxy`: each country runs through the Community proxy in that country. |
| `results.<country>` | `{status, partial?, url, markdown, html}` for a country that loaded — `url` is the final URL after redirects — and `{status: null, error}` for one that didn't. |
| `diff.markdown.<country>`, `diff.html.<country>` | A unified diff (`--- us`, `+++ de`, `@@` hunks) of each text format against the first country; `""` when the two are identical, `null` when this country or the baseline failed. The programs count its hunks and added/removed lines and pair the prices on removed and added lines (`$9.99 → £8.19`). |
| The HTML | `<html lang>` for the language and an "accept all" button label (`Accept all`, `Alle akzeptieren`, `Aceitar todos`, `すべて同意`, …) for a cookie banner; prices in the Markdown by currency symbol. |
| `x-call-id` + `GET /v1/me/sessions` | The call's record in the account's history: engine `function:geo`, its weight (one thread per country) and the thread-seconds billed. |
| One retry | When the baseline country failed, the call is made once more: every diff is against it, and then every diff comes back `null`. |
| A failure on purpose | The Steam page from `["us", "de"]` with `max_wait: 1`, Markdown only: no country can load it in a second, so the programs see what a failed call looks like. |

## How it works

1. For each page (a Steam store page, Google's home page, Nike's home page, example.com as the control), one geo call with the five countries — one page after another.
2. If the first country (`us`) didn't load, call once more and keep what the first attempt said.
3. Per country: status or error, `lang`, final URL, title, prices and their currencies, whether a cookie banner is in the HTML, sizes, and whether the Markdown is identical to the baseline's.
4. Per diff: bytes, hunks, lines added and removed, and the first price changes; per call: the time, the response size and how much of it is diff.
5. Make the call that is meant to fail (`max_wait: 1`) and read its errors and its diff.
6. Look each call up in the session history by its `x-call-id`: engine, weight and billed thread-seconds.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-10):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Page | Country | Result | lang | Final URL | Prices | Cookie banner | Markdown diff vs us | Call | Measured |
|---|---|---|---|---|---|---|---|---|---|
| Steam store page | us | 200 | en | https://store.steampowered.com/app/620/Portal_2/ | $9.99, $1.99 (USD) | no | baseline | 35.4 s · 175.9 thread-s · 3.0 MB, diffs 140 KB Markdown + 1.4 MB HTML | 2026-11-10 |
| Steam store page | de | 200 (partial) | de | https://store.steampowered.com/app/620/Portal_2/ | 9,69€, 1,59€ (EUR) | yes | 46 KB: +226 −224 lines; $9.99 → 9,69€, $1.99 → 1,59€, $19.98 → 19,38€ | — | 2026-11-10 |
| Steam store page | gb | 200 | en | https://store.steampowered.com/app/620/Portal_2/ | £8.19, £1.69 (GBP) | yes | 3.3 KB: +20 −12 lines; $9.99 → £8.19, $1.99 → £1.69, $19.98 → £16.38 | — | 2026-11-10 |
| Steam store page | jp | 200 | ja | https://store.steampowered.com/app/620/Portal_2/ | ¥ 1,400, ¥ 205 (JPY) | no | 48 KB: +179 −179 lines; $9.99 → ¥ 1,400, $1.99 → ¥ 205, $19.98 → ¥ 2,800 | — | 2026-11-10 |
| Steam store page | br | 200 | pt-br | https://store.steampowered.com/app/620/Portal_2/ | R$ 33,99, R$ 4,49 (BRL) | no | 42 KB: +171 −171 lines; $9.99 → R$ 33,99, $1.99 → R$ 4,49, $19.98 → R$ 67,98 | — | 2026-11-10 |
| Google home page | us | 200 | en | https://www.google.com/ | — | no | baseline | 33 s · 164.1 thread-s · 3.2 MB, diffs 22 KB Markdown + 1.7 MB HTML | 2026-11-10 |
| Google home page | de | 200 (partial) | de | https://www.google.com/ | — | yes | 8.5 KB: +141 −16 lines | — | 2026-11-10 |
| Google home page | gb | 200 | en-GB | https://www.google.com/ | — | yes | 6.8 KB: +137 −13 lines | — | 2026-11-10 |
| Google home page | jp | 200 | ja | https://www.google.com/ | — | no | 3.7 KB: +18 −16 lines | — | 2026-11-10 |
| Google home page | br | 200 | pt-BR | https://www.google.com/ | — | no | 3.5 KB: +17 −17 lines | — | 2026-11-10 |
| Nike home page | us | 200 | en-US | https://www.nike.com/ | — | no | baseline | 46.8 s · 231.9 thread-s · 13.6 MB, diffs 176 KB Markdown + 7.7 MB HTML | 2026-11-10 |
| Nike home page | de | 200 (partial) | de-DE | https://www.nike.com/de/ | 50 €, £50 (EUR, GBP) | no | 53 KB: +494 −810 lines | — | 2026-11-10 |
| Nike home page | gb | page_timeout | — | — | — | — | null (no comparison) | — | 2026-11-10 |
| Nike home page | jp | 200 (partial) | ja-JP | https://www.nike.com/jp/ | — | no | 56 KB: +606 −792 lines | — | 2026-11-10 |
| Nike home page | br | 200 | en-US | https://www.nike.com/ | — | no | 67 KB: +596 −592 lines | — | 2026-11-10 |
| example.com (control) | us | 200 | en | https://example.com/ | — | no | baseline | 7.1 s · 35.1 thread-s · 19 KB, diffs 0 bytes Markdown + 0 bytes HTML | 2026-11-10 |
| example.com (control) | de | 200 | en | https://example.com/ | — | no | "" (identical) | — | 2026-11-10 |
| example.com (control) | gb | 200 | en | https://example.com/ | — | no | "" (identical) | — | 2026-11-10 |
| example.com (control) | jp | 200 | en | https://example.com/ | — | no | "" (identical) | — | 2026-11-10 |
| example.com (control) | br | 200 | en | https://example.com/ | — | no | "" (identical) | — | 2026-11-10 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **One call, five countries, one bill per country.** On 2026-11-10 (re-run after the Functions fixes of 2026-11-08), across five runs (one per language), every call returned 200 with a result for each country and was in the history as `function:geo` with weight 5: billed 5 × its wall time, never more. A call takes as long as its slowest country — example.com 6.5–24.0 s, Google 17.6–47.1 s, Steam 35.4–46.5 s, Nike 34.2–47.4 s — so the four pages cost 605–675 thread-seconds a run, 10–11 thread-minutes (850 in the run that had to retry Steam).
- **The control is clean.** example.com came back byte-identical from all five countries in every run, and every diff was `""`: what the diff shows on the other pages comes from the sites, not from the Function.
- **Prices and currency: the diff hands you the changes.** Steam's page priced Portal 2 at $9.99, 9,69€, £8.19, ¥ 1,400 and R$ 33,99 in every run, in English, German, Japanese and Brazilian Portuguese, and the Markdown diff's removed and added lines paired them directly (`$9.99 → £8.19`, `$1.99 → ¥ 205`).
- **When the language changes, the diff is the whole page — and sometimes when it doesn't.** The German, Japanese and Portuguese pages gave diffs of 43–66 KB in 1–2 hunks, because every line is translated. The UK page is in English, but its diff depended on which US page came back: Steam served the US exit a 23 KB page in three runs and a 38 KB one in two; against the short one the UK diff was 3.4 KB in 9 hunks with every price change in it, against the long one 52–55 KB in 2 hunks. Read prices from each country's Markdown, and use the diff where both pages have the same layout.
- **Cookie banners follow where the site places you, not the country you asked for.** Steam showed its "Accept All / Reject All" popup on every page priced in euros or pounds and on none priced in dollars, yen or reais — it is in the HTML, not in the Markdown. Google's "Before you continue" dialog appeared exactly where its `lang` put the visitor in the EU or UK (`de`, `en-GB`) and roughly tripled the Markdown (5,585–6,927 bytes against 1,590–2,030 from the US) in every run. Nike showed its "Data improves your experience" consent from the UK in three of the four runs where the UK loaded, and never from Germany.
- **Country redirects are in `url` now.** Each country that loaded has its final URL: Nike sent Germany, the UK and Japan to `/de/`, `/gb/` and `/jp/`, and the US and Brazil to the global `www.nike.com/` (in English); Steam, Google and example.com kept the URL that was asked for. Like the banners, the redirect follows where the site places the exit: in one run Nike sent the German exit to `/gb/`, in English and pounds. (Before 2026-11-08 geo returned no URL and the programs read `<link rel="canonical">` from the HTML.) A currency regex finds what is on the page, not what you pay: Nike's German page also carried "£50" next to "50 €".
- **A failed country's diff is `null`, not `""`.** Five of the 105 country loads ended in `page_timeout` (Nike from Germany and the UK, Steam from the UK and the US, Google from Brazil); the call was still 200, the country came back as `{status: null, error: "page_timeout"}`, and its diff was `null` — distinct from `""` for an identical page. When the baseline failed (Steam from the US, once), every diff in that call was `null`, and the retry got all five. The call made to fail with `max_wait: 1` gave `page_timeout` for both countries and `diff: {de: null}` in every run.
- **Ask only for the formats you need.** With `html`, Nike's response was 14.2–19.2 MB, 8.1–11.2 MB of it HTML diffs, against 180–260 KB of Markdown diffs; over the four pages the HTML diffs were 28–33× the Markdown ones, and mostly markup. `markdown` (or `text`) gives a diff you can read; add `html` only for what Markdown drops — `lang`, a banner.
