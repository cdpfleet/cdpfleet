# Cases

Hard, real browser-automation problems solved end to end — what we tried, which options we used and why, the code in Node.js, Python, Java, C# and Go, and the real output. Every case is run on the production fleet in all five languages before it's published. They're also on [cdpfleet.com/docs/cases](https://cdpfleet.com/docs/cases); new ones are added regularly.

| # | Case | Difficulty | Browsers |
|---|---|---|---|
| #30 | [Five pages at once: extracting data in parallel within one browser session](30-parallel-pages) | Easy | Chromium |
| #29 | [Visual capture: full-page screenshot, element crop and PDF in one session](29-screenshot-and-pdf) | Easy | Chromium |
| #28 | [Scrape a catalogue page by page: structured data from rendered HTML](28-structured-extraction) | Easy | Chromium |
| #27 | [Watch it live: streaming a remote browser to your code, frame by frame](27-live-view) | Easy | Chromium, Firefox |
| #26 | [Same Chrome, two doors: Playwright's protocol (wsUrl) or CDP (cdpUrl)](26-playwright-vs-cdp) | Medium | Google Chrome |
| #25 | [No proxy of your own: rotating, country and sticky residential IPs with one token](25-residential-without-a-proxy) | Easy | Chromium |
| #24 | [New tabs, window.open and the three blocking dialogs: everything that interrupts a script](24-popups-and-dialogs) | Easy | Chromium |
| #23 | [Download every image on a page without downloading it twice](23-image-download) | Easy | Chromium |
| #22 | [Check every link on a page: 73 links in seconds, from inside the browser](22-link-checker) | Easy | Chromium |
| #21 | [Four ways to make a request, four different visitors: goto, in-page fetch, page.request and your own client](21-proxy-paths) | Medium | Chromium |
| #20 | [Three visitors, one thread: browser contexts instead of sessions](20-contexts-not-sessions) | Easy | Chromium |
| #19 | [Fetch first, browser second: paying for JavaScript only when the page needs it](19-fetch-first) | Easy | Chromium |
| #18 | [Session replay without a vendor: Playwright tracing and video on a remote browser](18-trace-and-video) | Easy | Chromium |
| #17 | [Infinite scroll, two ways: scroll like a user, or call the JSON the page calls](17-infinite-scroll) | Easy | Chromium |
| #16 | [What headless gives away, what headful fixes, and what neither does](16-headless-tells) | Medium | Chromium, Patchright, Camoufox |
| #15 | [Three countries, one script — and reading values the way the site does](15-three-countries) | Hard | Camoufox |
| #14 | [What your proxy costs you in page speed](14-proxy-speed) | Medium | Chromium |
| #13 | [Real-time sites through a proxy: WebSocket latency by proxy](13-websockets-through-proxies) | Medium | Chromium |
| #12 | [Files across the wire: uploading to and downloading from a remote browser](12-files-up-and-down) | Medium | Chromium |
| #11 | [Log in once, stay logged in: carrying a session between browsers](11-persist-login) | Easy | Chromium, Firefox |
| #10 | [A thread-aware worker pool: one session per page vs. reusing sessions](10-worker-pool) | Hard | Chromium |
| #9 | [Setting a header the wrong way: header order as a fingerprint](09-headers-and-h2) | Hard | Google Chrome |
| #8 | [A bandwidth diet for residential proxies: what blocking really saves](08-bandwidth-diet) | Medium | Chromium |
| #7 | [Different sites, different proxies, one browser: proxy_rules](07-per-host-proxy-rules) | Medium | Chromium |
| #6 | [Swap a proxy mid-session without losing the login — and the keep-alive trap](06-live-proxy-swap) | Hard | Chromium |
| #5 | [Four Chromium builds against common bot checks, headless and headful](05-stealth-builds) | Hard | Chromium, Google Chrome, Patchright, CloakBrowser |
| #4 | [A German Windows persona with Camoufox — and the one setting that broke it](04-camoufox-persona) | Hard | Camoufox |
| #3 | [Stable, beta, dev and an older major: four Chromes side by side](03-chrome-versions) | Easy | Google Chrome |
| #2 | [Emulating an iPhone on real Chrome: what changes, and what gives you away](02-mobile-emulation) | Medium | Google Chrome |
| #1 | [What your browser says before it says anything: TLS and HTTP/2 fingerprints](01-tls-fingerprints) | Medium | Google Chrome, Microsoft Edge, Firefox, Camoufox, WebKit |

## Environment

The programs read their configuration from environment variables:

| Variable | What |
|---|---|
| `CDPFLEET_API_KEY` | your API key (dashboard → API keys) |
| `PROXY_URL` | your proxy, e.g. `http://user:pass@proxy.example.com:8000` |
| `PROXY_URL_DE`, `PROXY_URL_US`, `PROXY_URL_JP` | proxy exits in Germany, the US, Japan (cases that compare countries) |
| `SOCKS_PROXIES` | comma-separated `socks5://user:pass@host:port` proxies (cases that use several proxies) |

Each case's README lists the ones it needs and how to install and run it in each language.
