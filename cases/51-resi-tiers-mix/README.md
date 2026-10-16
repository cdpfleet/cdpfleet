# Cut residential spend: the page on Tier 1, the images on Tier 4, in one session

**Medium** · 2026-10-16 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/resi-tiers-mix)

> The cdpfleet residential proxy has four price tiers, from $5 to $0.50 per GB. Send a gallery page through Tier 1 and its image hosts through Tier 4 with `proxy_rules`, count the bytes of each, and check them against what the balance actually billed per tier.

## The problem

Residential traffic is billed per GB, and on most pages the bulk of the bytes isn't the HTML you came for but images and static files from a CDN. The tier in the `cdpfleet-resi` token sets the price per GB, so the question is practical: can one session send the page through one tier and the heavy hosts through a cheaper one, how much of a real page is "heavy", and does the bill really follow the split?

## What we used, and why

| What | Why |
|---|---|
| `proxy: "cdpfleet-resi-country-us"` | The default route: every host no rule matches. No `-tier-N` means Tier 1 ($5/GB), US exits. |
| `proxy_rules` → `cdpfleet-resi-tier-4-country-us` | The two image hosts (`thumb.wikimedia.org`, `upload.wikimedia.org`) through Tier 4 ($0.50/GB). A token can be the session's `proxy` or a single rule's proxy (not part of a list of proxies). |
| `request.sizes()` on `requestfinished` | Headers and body of every request and response, as the browser saw them, added up per host group. |
| `GET /v1/me/proxy-balance` | `used_today` lists today's billed bytes per tier. Read before and after, the difference is what this run was billed — metering trails by about a minute, so the program waits for it. |

## How it works

1. Read today's billed bytes per tier.
2. Launch headless Chromium with Tier 1 as the default and a Tier 4 rule for the image hosts.
3. Open a Wikimedia Commons category gallery, scroll to the end so the lazy thumbnails load, and add up the bytes of the page host and of the image hosts.
4. Close the session, wait for metering, and compare the billed bytes per tier with what the browser counted; work out what 1,000 such loads cost on Tier 1 only and with the split.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-16):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Measured | Result |
|---|---|
| Requests | 223, of which 172 to the image hosts |
| Bytes seen by the browser | page host 849 KB (Tier 1), image hosts 1856 KB (Tier 4) — images 69 % |
| Bytes billed for the run | Tier 1 838 KB, Tier 4 1747 KB |
| 1,000 loads, all on Tier 1 | $13.53 |
| 1,000 loads, images on Tier 4 | $5.17 (−62 %) |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Most of the page was images:** about two thirds of the bytes came from the two image hosts — the HTML, CSS and JavaScript from the page's own host were the smaller part.
- **The bill followed the rule:** the Tier 4 bytes billed for the run matched the image bytes the browser counted, and the page's bytes landed on Tier 1. Each GB is charged at the tier of the token that carried it, so one session can mix prices.
- **The arithmetic:** with the images on Tier 4 a thousand loads of this page cost well under half of what they cost on Tier 1 alone (the table has the run's numbers). The saving is the image share times 90 % — the bigger the share of CDN bytes, the more a split pays.
- **Choose by what matters, not only by price:** keep the pages that decide whether a scrape works (logins, protected pages) on the tier you trust for them, and move bulk traffic — images, fonts, static files — to a cheaper one. The pools behind each tier can differ and may change; the targeting options (country, OS, sticky sessions) are the same on every tier.
- **Cheaper still:** bytes you never load cost nothing — block what you don't need ([case 08](https://cdpfleet.com/docs/cases/bandwidth-diet)). Tiers, balance and token syntax: [Residential proxy](https://cdpfleet.com/docs/proxies#residential).
