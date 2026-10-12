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
| `locator.waitFor({ state: "visible" })` | Waits up to 20 s for the banner and records how long after `domcontentloaded` it appeared. |
| `page.on("request")` | Counts the hosts on other domains the page contacted — ad, analytics and CDN hosts — up to each moment. |
| `context.cookies()` | All cookies in the context, including third-party ones a page script can't see. |
| `frameLocator('iframe[id^="sp_message_iframe"]')` | Sourcepoint draws its banner in an iframe; OneTrust's buttons (`#onetrust-reject-all-handler`, `#onetrust-accept-btn-handler`) are in the page. |

## How it works

1. Launch Chromium through a US proxy and load each site in a fresh context; wait for a banner (up to 20 s, at least 6 s in all) and record the platform, banner, consent APIs, cookies and other-domain hosts. Then close that session and launch one through a German proxy (an idle session on shared threads ends after 60 s, so the countries run one after the other).
2. From Germany, load each site twice in fresh contexts: record the same before any choice, then click the first-layer "reject" in one and "accept" in the other — clicking once more if the banner is still up 5 s later — and measure again 6 s after that.
3. A free "reject" counts only if the first layer offers one — "Reject all and subscribe" isn't clicked. A page that answers with an error status is loaded once more (the rotating proxy gives the retry another IP).

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-12):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_DE`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| Site | Exit / choice | Banner | Cookies | Third-party cookies | Other-domain hosts |
|---|---|---|---|---|---|
| theguardian.com | US · Sourcepoint, GPP | yes, after 9.3 s | 33 | 16 | 66 |
| theguardian.com | DE · before a choice · Sourcepoint, TCF | yes, after 4.1 s | 7 | 0 | 5 |
| theguardian.com | DE · reject: no reject button on the first layer |  | — | — | — |
| theguardian.com | DE · "Accept all" |  | 113 | 90 | 148 |
| bbc.com | US · Sourcepoint, GPP, TCF | no | 51 | 17 | 69 |
| bbc.com | DE · before a choice · Sourcepoint, TCF, GPP | yes, after 2.5 s | 18 | 2 | 16 |
| bbc.com | DE · "I do not agree" |  | 30 | 4 | 33 |
| bbc.com | DE · "I agree" |  | 44 | 15 | 48 |
| apnews.com | US · OneTrust, GPP | yes, after 9.0 s | 111 | 75 | 195 |
| apnews.com | DE · before a choice · OneTrust, TCF | yes, after 0.4 s | 26 | 3 | 72 |
| apnews.com | DE · "I Reject All (except Strictly Necessary)" |  | 35 | 3 | 79 |
| apnews.com | DE · "I Accept All" |  | 174 | 142 | 254 |
| theverge.com | US · OneTrust, GPP | no | 247 | 223 | 219 |
| theverge.com | DE · before a choice · OneTrust, TCF | yes, after 1.9 s | 8 | 0 | 34 |
| theverge.com | DE · "Reject Non-Essential" |  | 12 | 0 | 40 |
| theverge.com | DE · "Accept" |  | 48 | 14 | 87 |
| huffpost.com | US · no platform, GPP | no | 228 | 218 | 294 |
| huffpost.com | DE · before a choice · OneTrust, TCF | yes, after 0.7 s | 8 | 0 | 62 |
| huffpost.com | DE · "Reject All" |  | 11 | 0 | 65 |
| huffpost.com | DE · "I Accept" |  | 186 | 154 | 252 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The exit country picks the site you get.** From Germany all five sites showed a consent banner and exposed IAB Europe's TCF API in every run (five runs, one per language); the banner appeared 0.1–8.5 s after `domcontentloaded`. From the US, only the BBC exposed TCF (it runs both APIs everywhere); every site that loaded exposed IAB Tech Lab's GPP instead. US banners were the Guardian's privacy message (every run), HuffPost's (three of five runs) and AP News's (both runs where it loaded); the BBC and The Verge showed none.
- **Some sites refuse some exits.** AP News answered 403 to the US exit in three of five runs, even after a second try from a fresh context with a new IP; from Germany it loaded every time. Pick the exit for the site, and check the status before you parse.
- **A banner doesn't mean nothing loads.** Before any choice, the German visitor's page had already contacted 5 other-domain hosts on the Guardian, 15–16 on the BBC, 34–51 on The Verge, 48–63 on HuffPost and 65–96 on AP News — mostly with 0–2 third-party cookies, but AP set 3–9.
- **"Reject" changes little; "accept" changes a lot on three of the five.** After a reject, cookies rose by 3–15 and third-party cookies stayed at 0–2 on The Verge and HuffPost, 4 on the BBC and 3–13 on AP News. After "accept", the Guardian carried 60–99 third-party cookies, AP News 109–167 and HuffPost 121–157, with 128–285 other-domain hosts. On the BBC (9–15 against 4) and The Verge (9–16 against 0–2) accepting added far less.
- **From the US nothing is held back.** With no click at all, the US visitor carried up to 223 third-party cookies on The Verge and 218 on HuffPost in some runs — more than accepting everything from Germany gave on The Verge (9–16).
- **Not every banner has a free "no".** The Guardian's first layer had an "Accept all" but no free reject in any run — its alternative is pay-or-consent.
- **For scraping:** pick the exit country on purpose ([a country-pinned residential token](https://cdpfleet.com/docs/proxies#residential) or your own proxy), expect an overlay from the EU, and click the first-layer reject — `#onetrust-reject-all-handler` in the page, Sourcepoint's buttons inside `iframe[id^="sp_message_iframe"]`. Wait for the banner rather than sleeping a fixed time: it took up to 9.3 s on the Guardian from the US and 18.9 s on AP News. Check it's gone after the click and click again if not (no second click was needed in these runs, but consent iframes that re-render can swallow one).
