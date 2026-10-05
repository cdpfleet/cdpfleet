# cdpfleet — cloud browsers for Playwright: examples, cases and API docs

**Real browsers in the cloud, driven with Playwright.** One HTTPS call launches a remote browser — Chrome, Edge, Brave, Opera, Chromium, Firefox, Camoufox, WebKit, Patchright, Rebrowser and more, headless or headful — and returns a WebSocket URL. Connect with Playwright from **Node.js, Python, Java, C# or Go** and drive it like a local browser: web scraping, end-to-end testing, browser automation, fingerprint and proxy work, at scale and metered per second.

This repository holds runnable examples, real-world cases, starter templates and the API reference. Full documentation: **[cdpfleet.com/docs](https://cdpfleet.com/docs)**.

## Quickstart

```js
// npm install playwright@1.60.0
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// 1. Launch a browser (every session needs your proxy)
const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: 'http://user:pass@proxy.example.com:8080', headless: true }),
});
const { wsUrl } = await res.json();

// 2. Connect and drive it
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const page = await browser.newPage();
await page.goto('https://example.com');
console.log(await page.title());
await browser.close(); // ends the session and stops billing
```

The same in [Python, Java, C# and Go](examples/quickstart). Get an API key in the [dashboard](https://cdpfleet.com/app).

## What's here

| Folder | What |
|---|---|
| [docs/](docs) | Getting started, the launch and account APIs, proxies, sessions and limits, errors, and every browser's launch options |
| [examples/](examples) | Complete programs in five languages: quickstart, one per browser, and common recipes |
| [cases/](cases) | Hard, real problems solved end to end — fingerprints, proxies, files, WebSockets, worker pools — with code in five languages and real output |
| [templates/](templates) | Ready-to-run starter projects for Node.js, Python, Java, C# and Go |

## Good to know

- **Playwright protocol, not raw CDP.** Use `browserType.connect(wsUrl)` with the Playwright client of the same minor version as the server — **1.60** (1.52 for Rebrowser). `connectOverCDP()` and Puppeteer don't work.
- **Every session needs a proxy.** Pass yours as `proxy`; see [docs/proxies.md](docs/proxies.md) for formats, pools and per-host rules.
- **Billing is per second while a session runs.** `browser.close()` ends it; a session you never connect to ends after 60 seconds. See [docs/sessions-and-limits.md](docs/sessions-and-limits.md).

## Popular cases

- [What your TLS and HTTP/2 fingerprint looks like in each browser](cases/01-tls-fingerprints) (JA3/JA4, tls.peet.ws)
- [Mobile emulation: iPhone and Android on remote browsers](cases/02-mobile-emulation)
- [Swap a running browser's proxy without restarting it](cases/06-live-proxy-swap)
- [Route specific hosts through different proxies](cases/07-per-host-proxy-rules)
- [A worker pool with retries and backoff](cases/10-worker-pool)
- [Stay logged in across sessions with storageState](cases/11-persist-login)
- [Three countries, one script: Camoufox geo personas](cases/15-three-countries)
- [What headless gives away, what headful fixes, and what neither does](cases/16-headless-tells)
- [Session replay without a vendor: Playwright tracing and video](cases/18-trace-and-video)
- [Four ways to make a request, four different visitors: goto, in-page fetch, page.request](cases/21-proxy-paths)

All 21 in [cases/](cases).

## Support

Questions, bugs in the examples or ideas for a case: open an issue, or write to the address on [cdpfleet.com](https://cdpfleet.com).
