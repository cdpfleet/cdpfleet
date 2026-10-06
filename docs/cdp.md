# CDP & Puppeteer

Add `"cdp": true` to a launch and the response carries `cdpUrl` next to `wsUrl`: a browser-level Chrome DevTools Protocol WebSocket. Drive it with Puppeteer, Playwright's `connectOverCDP()` (any language, any recent version) or any raw CDP client — Stagehand, browser-use, chromedp, Rod.

```
POST https://starter.cdpfleet.com/chrome/session
{ "proxy": "http://user:pass@proxy.example.com:8080", "headless": "new", "cdp": true }

→ { "sessionId": "…", "wsUrl": "wss://router01.cdpfleet.com/session/…", "cdpUrl": "wss://router01.cdpfleet.com/session/…/cdp", … }
```

```js
// npm install puppeteer-core
import puppeteer from 'puppeteer-core';
const browser = await puppeteer.connect({ browserWSEndpoint: cdpUrl, headers: { 'x-api-key': process.env.CDPFLEET_API_KEY } });
const page = await browser.newPage();
await page.goto('https://example.com');
await browser.close(); // ends the session
```

Playwright in five languages: [examples/recipes/chrome-cdp](../examples/recipes/chrome-cdp). Raw clients can pass the key as `?api_key=…`.

- **Browsers:** Chrome, Edge, Brave, Opera, Yandex, Chromium, CloakBrowser. Others return `400` listing the supported ones — Camoufox, Firefox and WebKit have no CDP; Patchright and Rebrowser keep their stealth in the Playwright layer CDP bypasses; Whale crashes when a CDP client opens a tab.
- **Same session:** same proxy, `proxy_rules`, live swap, timeouts, threads and billing.
- **Both at once:** `wsUrl` and `cdpUrl` can be connected together, but disconnecting either ends the session.
- **Auth:** only the launching key (other keys → `401`/`403`); `/cdp` on a session launched without `cdp` → `404`.
- **Detection:** CDP clients that call `Runtime.enable` (Puppeteer does) are detectable; stay on `wsUrl` with a stealth build when that matters.

Full page: [cdpfleet.com/docs/cdp](https://cdpfleet.com/docs/cdp).
