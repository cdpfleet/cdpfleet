# Launch & connect API

## Launch a browser

```
POST https://starter.cdpfleet.com/<browser>/session
x-api-key: <your key>
content-type: application/json

{ "proxy": "http://user:pass@proxy.example.com:8080", "headless": true }
```

`<browser>` is one of the endpoints in [browsers-and-options.md](browsers-and-options.md) (`chrome`, `edge`, `brave`, `opera`, `whale`, `yandex`, `chromium`, `patchright`, `rebrowser`, `cloakbrowser`, `camoufox`, `firefox`, `webkit`). The body holds that browser's launch options; **`proxy` is required**.

### Response

```json
{
  "sessionId": "46903d7c-87d2-4320-b85b-cf109c0ec984",
  "wsUrl": "wss://router01.cdpfleet.com/session/46903d7c-…",
  "browser": "chromium",
  "resource_class": "linux_headless",
  "weight": 1,
  "expires_at": "2026-10-02T12:34:56Z"
}
```

- `wsUrl` — connect to it within 60 seconds. It can be used once.
- `resource_class` / `weight` — how many threads the session takes (headless 1, headful 2).
- `expires_at` — when the session ends at the latest (`overall_timeout`, capped by your plan).

## Connect

```js
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
```

Use Playwright's `connect()` with the client for the browser's family — `chromium` for the Chromium-based browsers, `firefox` for Firefox and Camoufox, `webkit` for WebKit — and the same key that launched the session.

## Connect over CDP (Puppeteer, connectOverCDP)

Launch with `"cdp": true` (Chrome, Edge, Brave, Opera, Yandex, Chromium, CloakBrowser) and the response also has `cdpUrl` (`<wsUrl>/cdp`). See [cdp.md](cdp.md).

```js
const browser = await puppeteer.connect({ browserWSEndpoint: cdpUrl, headers: { 'x-api-key': KEY } });
// or: await chromium.connectOverCDP(cdpUrl, { headers: { 'x-api-key': KEY } });
```

## End a session

`browser.close()` ends it at once. A session also ends when either side closes the WebSocket, after `inactivity_timeout` without traffic (60 s by default), at `overall_timeout`, or when you end it from the dashboard or the [Account API](account-api.md).

## Change the proxy of a running session

```
POST https://<host of your wsUrl>/admin/session/proxy
x-api-key: <your key>
{ "session_id": "46903d7c-…", "proxy": "http://user:pass@new-exit.example.com:8080" }
```

Every session is swappable. The new proxy applies to new connections — see [proxies.md](proxies.md#live-swap) and the [live proxy swap case](../cases/06-live-proxy-swap).
