# Block analytics and beacons, keep the page: what telemetry a scraper sends without knowing

**Easy** · 2026-10-18 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/intercept-and-block-telemetry)

> Load three real sites with and without a telemetry block list (analytics, measurement and real-user-monitoring hosts, plus beacons). Count what each page reports about your visit — including the beacons it fires as you leave — and check the content stays the same.

## The problem

Every page a scraper opens also reports the visit: analytics hits, measurement pixels, performance beacons, session counters. That traffic goes out through your proxy, tells the site's analytics about your session, and keeps going until the moment you leave — some beacons are only sent when the page is hidden or closed. Blocking everything third-party (as in the [bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet)) saves the most bytes but can break pages that need a consent or anti-bot script. A narrow telemetry list is the safe middle: does it keep the page intact, and how much does it actually stop?

## What we used, and why

| What | Why |
|---|---|
| A telemetry host list | Analytics, measurement and real-user-monitoring services (Google Analytics and Tag Manager, comScore, Akamai mPulse, New Relic, Sentry, Datadog, Hotjar, Chartbeat, Adobe, the Guardian's own Ophan…), subdomains included. Ads aren't on it: this is about what reports your visit, not what sells it. |
| `request.resourceType() === 'ping'` | Catches beacons from any host: `navigator.sendBeacon` and `<a ping>` requests are typed `ping`. |
| `context.route('**/*')` + `route.abort()` | Drops telemetry before it leaves the browser; everything else continues. |
| `request` events and `request.sizes()` | Count every request and the telemetry among them; bytes on the wire where Chromium reports them. |
| Leaving the page (`goto('about:blank')`) | Fires the page-hide beacons a closing tab sends; they're counted separately. |
| A fresh context per load | Empty cache and cookies, so both loads of a site start the same. |

## How it works

1. For each site (The Guardian's international front, NPR's home page, a Wikipedia article): load it in a fresh context, wait for DOM ready plus 6 s, read the title and count the headings, then leave the page and wait 1.5 s.
2. Count all requests, the telemetry requests, the telemetry sent while leaving, and bytes.
3. Load it again in a fresh context with telemetry blocked, and count what was blocked and whether the title and headings are the same.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-18):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| Site | Requests | Telemetry | …sent while leaving | Telemetry KB | Total KB | Blocked (2nd load) | Headings | Headings, blocked | DOM ready (ms) | DOM ready, blocked (ms) |
|---|---|---|---|---|---|---|---|---|---|---|
| The Guardian | 144 | 24 | 3 | 1 | 1628 | 24 | 182 | 182 | 2012 | 2416 |
| NPR | 66 | 3 | 0 | 198 | 2824 | 8 | 61 | 61 | 6055 | 4420 |
| Wikipedia | 40 | 1 | 0 | 2 | 592 | 1 | 12 | 12 | 4125 | 1628 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **News pages report a lot about each visit.** The Guardian sent 13–43 of its 93–199 requests to analytics and measurement services across the five runs — its own analytics (Ophan) fires a request for nearly everything that happens on the page — and NPR 3–19 of 66–253. A Wikipedia article sent a single event beacon of about 2 KB.
- **Telemetry keeps going as you leave.** After the scraper navigated away, the Guardian sent 1–4 more telemetry requests in every run and NPR up to 2 — a script that closes its tabs still reports the visit on the way out. Blocking stops those too.
- **Blocking it didn't change the content.** Every site kept its title, and the headings matched the unblocked load within 3 (front pages rotate between loads).
- **It's about requests and privacy more than bytes.** Telemetry weighed 0–561 KB on the Guardian, depending on which measurement scripts were served, and about 200 KB on NPR (mostly Tag Manager and Akamai mPulse), out of 1–7 MB pages; fire-and-forget beacons report no size at all. To cut bandwidth, block images and third parties ([bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet)); to stop reporting your sessions, block this list.
- Counts move from load to load — what fires depends on the ads, A/B tests and consent state served that time. A blocked beacon can be retried, so the Guardian's blocked count (21–24) was often above what an unblocked load sent.
