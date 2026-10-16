# Cases

Hard, real browser-automation problems solved end to end — what we tried, which options we used and why, the code in Node.js, Python, Java, C# and Go, and the real output. Every case is run on the production fleet in all five languages before it's published. They're also on [cdpfleet.com/docs/cases](https://cdpfleet.com/docs/cases); new ones are added regularly.

| # | Case | Difficulty | Browsers |
|---|---|---|---|
| #53 | [Surviving a soft block: honour Retry-After, then move to another exit without losing the login](53-soft-block-backoff) | Medium | Chromium |
| #52 | [Installed fonts as a fingerprint: what a width probe sees on each Camoufox persona](52-fonts-fingerprint) | Medium | Camoufox |
| #51 | [Cut residential spend: the page on Tier 1, the images on Tier 4, in one session](51-resi-tiers-mix) | Medium | Chromium |
| #50 | [A price watcher in one program: two snapshots minutes apart, diffed, billed only while reading](50-price-watch) | Easy | Chromium |
| #49 | [The same Camoufox device across relaunches: what a persistent profile keeps, and what it doesn't](49-camoufox-same-device) | Medium | Camoufox |
| #48 | [Mock, patch and fail API responses with route.fulfill on remote browsers](48-mock-api-responses) | Medium | Chromium, Firefox, WebKit |
| #47 | [A first free scrape on the Community plan: no proxy to bring, country and sticky exits included](47-community-first-scrape) | Easy | Chromium |
| #46 | [Browser extensions aren't supported — what to use instead](46-no-extensions) | Easy | Google Chrome, Firefox |
| #45 | [Clipboard and file permissions on remote browsers: what's blocked and what works](45-clipboard-and-files) | Medium | Chromium, Google Chrome, Firefox, WebKit |
| #44 | [Print to PDF across browsers: which engines can, and the fallback for the rest](44-print-to-pdf) | Easy | Chromium, Google Chrome, Microsoft Edge, Firefox, Camoufox, WebKit |
| #43 | [Can a page tell your automation client is listening? CDP and the console](43-cdp-detectability) | Medium | Chromium, Patchright, Google Chrome, Camoufox |
| #42 | [Puppeteer on real Google Chrome: cdpUrl, raw CDP and request interception](42-puppeteer-on-real-chrome) | Easy | Google Chrome |
| #41 | [A scrape broke: find out why from its session recording](41-replay-a-failed-run) | Easy | Chromium |
| #40 | [Persistent profiles: what a saved browser profile keeps, and what it doesn't](40-persistent-profiles) | Easy | Chromium |
| #39 | [Saved the login, still logged out: storageState and IndexedDB](39-indexeddb-login-state) | Medium | Chromium |
| #38 | [Logging in through a TOTP two-factor prompt from a remote browser](38-totp-login) | Medium | Chromium |
| #37 | [Cookie banners by country: the same five sites from Germany and from the US](37-cookie-consent-by-country) | Medium | Chromium |
| #36 | [Slow 3G on a remote browser: network throttling over a CDP session](36-slow-network) | Easy | Chromium |
| #35 | [A German IP with a UTC clock: making location, timezone and permissions agree](35-geolocation-and-permissions) | Easy | Chromium |
| #34 | [Record a page once, replay it offline: HAR files with a remote browser](34-har-record-and-replay) | Medium | Chromium |
| #33 | [When is a page ready? commit, domcontentloaded, load, networkidle or a selector](33-wait-strategies) | Easy | Chromium |
| #32 | [A page for an LLM: HTML, innerText or the accessibility snapshot?](32-page-to-text-for-llms) | Easy | Chromium |
| #31 | [Content you can't querySelector: iframes and shadow DOM](31-iframes-and-shadow-dom) | Medium | Chromium |
| #30 | [Five pages at once: parallel tabs in one browser session](30-parallel-pages) | Easy | Chromium |
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
| #5 | [Three Chromium builds against common bot checks, headless and headful](05-stealth-builds) | Hard | Chromium, Google Chrome, Patchright |
| #4 | [A German Windows persona with Camoufox — and the one setting that broke it](04-camoufox-persona) | Hard | Camoufox |
| #3 | [Stable, beta, dev and an older major: four Chromes side by side](03-chrome-versions) | Easy | Google Chrome |
| #2 | [Emulating an iPhone on real Chrome: what changes, and what gives you away](02-mobile-emulation) | Medium | Google Chrome |
| #1 | [What your browser says before it says anything: TLS and HTTP/2 fingerprints](01-tls-fingerprints) | Medium | Google Chrome, Microsoft Edge, Firefox, Camoufox, WebKit |

## Environment

The programs read their configuration from environment variables:

| Variable | What |
|---|---|
| `CDPFLEET_API_KEY` | your API key (dashboard → API keys) |
| `COMMUNITY_API_KEY` | an API key of an account on the free Community plan (cases about that plan) |
| `PROXY_URL` | your proxy, e.g. `http://user:pass@proxy.example.com:8000` |
| `PROXY_URL_DE`, `PROXY_URL_US`, `PROXY_URL_JP` | proxy exits in Germany, the US, Japan (cases that compare countries) |
| `SOCKS_PROXIES` | comma-separated `socks5://user:pass@host:port` proxies (cases that use several proxies) |

Each case's README lists the ones it needs and how to install and run it in each language.
