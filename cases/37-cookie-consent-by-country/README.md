# Cookie banners by country: the same five sites from Germany and from the US

**Medium** · 2026-10-12 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cookie-consent-by-country)

> The Guardian, BBC, AP News, The Verge and HuffPost through a German and a US exit: which show a consent banner, which consent API they expose, what loads before anyone clicks, and what "reject" and "accept" change.

## The problem

Your scraper's exit country decides which version of a site it gets. From the EU most news and media sites put a consent banner over the page, run IAB Europe's TCF consent API and are supposed to hold back ad and analytics tags until you choose; from the US the same sites often show nothing and load everything. That changes what you see (an overlay hiding the content), how much bandwidth a page costs and which cookies your session carries afterwards. How big is the difference, and what do "reject" and "accept" actually do?

## What we used, and why

| What | Why |
|---|---|
| `PROXY_URL_DE` and `PROXY_URL_US` | One headful Chromium per country; every site is loaded in a fresh context from each. |
| A probe in `page.evaluate` | Detects the consent platform (OneTrust, Sourcepoint, Didomi, Cookiebot, Usercentrics), whether a OneTrust or Sourcepoint banner is visible, and the consent APIs the page exposes: `__tcfapi` (IAB Europe's TCF) and `__gpp` (IAB Tech Lab's GPP, used for US state privacy laws). |
| `locator.waitFor({ state: "visible" })` | Waits up to 30 s for the banner (on 2026-10-21 HuffPost's took 28 s from Germany) and records how long after `domcontentloaded` it appeared. |
| `page.on("request")` | Counts the hosts on other domains the page contacted — ad, analytics and CDN hosts — up to each moment. |
| `context.cookies()` | All cookies in the context, including third-party ones a page script can't see. |
| `frameLocator('iframe[id^="sp_message_iframe"]')` | Sourcepoint draws its banner in an iframe; OneTrust's buttons (`#onetrust-reject-all-handler`, `#onetrust-accept-btn-handler`) are in the page. |

## How it works

1. Launch Chromium through a US proxy and load each site in a fresh context; wait for a banner (up to 30 s, at least 6 s in all) and record the platform, banner, consent APIs, cookies and other-domain hosts. Then close that session and launch one through a German proxy (an idle session ends after 2 minutes by default, so the countries run one after the other).
2. From Germany, load each site twice in fresh contexts: record the same before any choice, then click the first-layer "reject" in one and "accept" in the other — clicking once more if the banner is still up 5 s later — and measure again 6 s after that.
3. A free "reject" counts only if the first layer offers one — "Reject all and subscribe" isn't clicked. A page that answers with an error status is loaded once more (the rotating proxy gives the retry another IP).

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-21):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| Site | Exit / choice | Banner | Cookies | Third-party cookies | Other-domain hosts |
|---|---|---|---|---|---|
| theguardian.com | US · Sourcepoint, GPP | yes, after 3.5 s | 17 | 5 | 46 |
| theguardian.com | DE · before a choice · Sourcepoint, TCF | yes, after 2.0 s | 7 | 0 | 5 |
| theguardian.com | DE · reject: no reject button on the first layer |  | — | — | — |
| theguardian.com | DE · "Accept all" |  | 102 | 80 | 146 |
| bbc.com | US · Sourcepoint, GPP, TCF | yes, after 4.3 s | 18 | 4 | 14 |
| bbc.com | DE · before a choice · Sourcepoint, TCF, GPP | yes, after 7.3 s | 18 | 2 | 15 |
| bbc.com | DE · "I do not agree" |  | 30 | 4 | 38 |
| bbc.com | DE · "I agree" |  | 31 | 6 | 31 |
| apnews.com | US · no platform | page refused (403) | 1 | 0 | 2 |
| apnews.com | DE · before a choice · OneTrust, TCF | yes, after 4.9 s | 36 | 15 | 112 |
| apnews.com | DE · "I Reject All (except Strictly Necessary)" |  | 42 | 15 | 121 |
| apnews.com | DE · "I Accept All" |  | 192 | 165 | 282 |
| theverge.com | US · OneTrust, GPP | no | 328 | 291 | 285 |
| theverge.com | DE · before a choice · OneTrust, TCF | yes, after 1.8 s | 7 | 0 | 33 |
| theverge.com | DE · "Reject Non-Essential" |  | 10 | 0 | 38 |
| theverge.com | DE · "Accept" |  | 41 | 14 | 74 |
| huffpost.com | US · OneTrust, GPP | yes, after 3.8 s | 76 | 67 | 170 |
| huffpost.com | DE · before a choice · OneTrust, TCF | yes, after 6.7 s | 9 | 0 | 55 |
| huffpost.com | DE · "Reject All" |  | 12 | 0 | 66 |
| huffpost.com | DE · "I Accept" |  | 149 | 118 | 261 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The exit country picks the site you get.** Measured again on 2026-10-21 (five runs, one per language): from Germany all five sites showed a consent banner and exposed IAB Europe's TCF API in four runs; in the fifth The Verge showed neither. The banner appeared 0.2–28.1 s after `domcontentloaded`. From the US, only the BBC exposed TCF (it runs both APIs everywhere); every site that loaded exposed IAB Tech Lab's GPP instead. US banners: the Guardian's privacy message in four runs, the BBC's and HuffPost's in three each, none on AP News (in the two runs where it loaded) or The Verge.
- **Some sites refuse some exits.** AP News answered 403 to the US exit in three of five runs, even after a second try from a fresh context with a new IP; from Germany it loaded every time. Pick the exit for the site, and check the status before you parse.
- **A banner doesn't mean nothing loads.** Before any choice, the German visitor's page had already contacted 5–6 other-domain hosts on the Guardian, 13–15 on the BBC, 29–48 on The Verge, 45–71 on HuffPost and 75–137 on AP News — with 0–2 third-party cookies, except AP News at 4–19.
- **"Reject" changes little; "accept" changes a lot on three of the five.** After a reject, cookies rose by 0–15 and third-party cookies stayed at 0–2 on The Verge and HuffPost, 2–4 on the BBC and 4–22 on AP News. After "accept", the Guardian carried 78–99 third-party cookies, AP News 131–165 and HuffPost 87–166, with 133–282 other-domain hosts. On the BBC (6–14 against 2–4) and The Verge (14–17 against 0–2) accepting added far less.
- **From the US nothing is held back.** With no click at all, the US visitor carried up to 291 third-party cookies on The Verge, 249 on HuffPost and 248 on AP News in some runs — far more than accepting everything from Germany gave on The Verge (14–17).
- **Not every banner has a free "no".** The Guardian's first layer had an "Accept all" but no free reject in any run — its alternative is pay-or-consent.
- **For scraping:** pick the exit country on purpose ([a country-pinned residential token](https://cdpfleet.com/docs/proxies#residential) or your own proxy), expect an overlay from the EU, and click the first-layer reject — `#onetrust-reject-all-handler` in the page, Sourcepoint's buttons inside `iframe[id^="sp_message_iframe"]`. Wait for the banner rather than sleeping a fixed time, and wait long: it took up to 20.0 s on HuffPost from the US and 28.1 s from Germany, and 21.9 s on AP News from Germany. Check it's gone after the click and click again if not (no second click was needed in these runs, but one "accept" click on The Verge changed nothing — consent iframes that re-render can swallow one).
