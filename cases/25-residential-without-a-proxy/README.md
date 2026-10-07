# No proxy of your own: rotating, country and sticky residential IPs with one token

**Easy** · 2026-10-07 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/residential-without-a-proxy)

> Four launches on the cdpfleet residential proxy — any country, Germany, and one sticky US identity carried from one browser session to the next — with nothing but a token as the proxy.

## The problem

Every cdpfleet session needs a proxy, and until now that meant buying one somewhere else first. Residential IPs are what most scraping and account work needs: real ISP addresses in the right country, a new one per request for crawling, or the same one for a whole login flow. The [cdpfleet residential proxy](https://cdpfleet.com/docs/proxies#residential) puts all of that in the `proxy` field — `cdpfleet-resi`, plus country, device OS and a sticky session name — billed from a prepaid balance. Does the targeting hold, does a sticky IP really survive into the next browser, and what does it look like from the site's side?

## What we used, and why

| What | Why |
|---|---|
| `"proxy": "cdpfleet-resi"` | A new residential IP for every connection, any country. No credentials anywhere in your code: the upstream is configured on our servers. |
| `-country-de` | Exit in Germany; any ISO two-letter code works. |
| `-session-<name>-lifetime-10` | A sticky session: every connection with the same name uses the same IP for up to 10 minutes. Names are private to your account. |
| Two browser sessions with the same sticky name | Browser 2 starts after browser 1 has closed — the question is whether the identity carries over. |
| `http://ip-api.com/json` three times per browser | Three separate connections: rotating exits should change, sticky ones shouldn't. Each lookup is retried — residential peers drop a few percent of connections. |
| `GET /v1/me/proxy-balance` | The prepaid balance the traffic is billed to ($5/GB, 1 GB free on every account). |

## How it works

1. Launch Chromium four times with four tokens: `cdpfleet-resi`, `cdpfleet-resi-country-de`, and twice `cdpfleet-resi-country-us-session-<name>-lifetime-10`.
2. In each browser, look up the exit IP and country three times.
3. Compare: how many distinct IPs each browser saw, which countries, and whether the second sticky browser got the first one's IP.
4. Read the balance and price from the Account API.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-07):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Launch | proxy | Exit countries | IPs in 3 lookups | Same IP as browser 1 |
|---|---|---|---|---|
| rotating, any country | cdpfleet-resi | IT,FR,IN | 3 | — |
| rotating, Germany | cdpfleet-resi-country-de | DE | 3 | — |
| sticky US, browser 1 | cdpfleet-resi-country-us-session-<name>-lifetime-10 | US | 1 | true |
| sticky US, browser 2 | cdpfleet-resi-country-us-session-<name>-lifetime-10 | US | 1 | true |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The token is the whole proxy setup:** no account with a proxy vendor, no credentials in code, nothing to rotate — and the same token works in all five languages and in `proxy_rules`.
- **Rotating means rotating:** three lookups gave three different IPs, in a different country each time when no country was set, and in Germany with `-country-de` — in all but one of our runs, where a geo-IP database placed one German exit elsewhere. Databases disagree about residential ranges; check the country with the service the site itself is likely to use.
- **Sticky means sticky, across browsers:** both US sessions saw one IP each — and the same IP as each other. Reuse the name to keep an identity through a login in one browser and the work in the next.
- **Billing is by traffic, not by the token:** a lookup is a few kilobytes; a full page with images is hundreds. Use [bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet) techniques or `proxy_rules` to send only the requests that need a residential IP through it.
- **Plan for failures:** residential connections belong to real people's devices and a few percent fail — retry a request, and retry a launch that returns `503`.
