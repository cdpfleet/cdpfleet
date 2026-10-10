# cdpfleet — Node.js SDK

Launch real remote browsers on [cdpfleet](https://cdpfleet.com) and drive them with your own Playwright.

```bash
npm install cdpfleet playwright@1.60.0
```

```js
import { chromium } from 'playwright';
import { Cdpfleet } from 'cdpfleet';

const fleet = new Cdpfleet(); // reads CDPFLEET_API_KEY
const session = await fleet.launch('chrome', { proxy: 'http://user:pass@proxy.example.com:8080', headless: false });
const browser = await session.connect(chromium);
const page = await browser.newPage();
await page.goto('https://example.com');
console.log(await page.title());
await session.close(); // ends the session and stops billing
```

- `launch` retries 503 (fleet busy) and 429 (rate/thread limits) as the API asks, honouring `Retry-After`.
- `session.connectOverCDP(chromium)` for sessions launched with `cdp: true`.
- **keep_alive** (dedicated threads): call `session.connect()` again after a disconnect to re-attach; `session.close()` ends the session.
- `session.liveLink('control')`, `session.swapProxy(proxy, rules)`, and the account API: `fleet.usage()`, `threads()`, `sessions()`, `recordings()`, `recordingUrl(id)`, `proxyBalance()`.
- Playwright is a peer dependency: you choose the client (1.60; 1.52 for Rebrowser).

Docs: https://cdpfleet.com/docs/sdk · Spec: [../SPEC.md](../SPEC.md)
