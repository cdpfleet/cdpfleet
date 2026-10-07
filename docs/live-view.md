# Live view

Watch a remote browser while your code drives it. Playwright 1.60's `page.screencast` streams a JPEG frame to your code whenever the page changes, over the session's own connection — nothing to enable at launch.

```js
await page.screencast.start({ quality: 60, onFrame: ({ data, viewportWidth, viewportHeight }) => {
  // data: JPEG bytes — forward to your UI, a websocket, a file…
} });
await page.goto('https://example.com');
await page.screencast.stop();
```

All five languages: [examples/recipes/live-view](../examples/recipes/live-view). Add `path: 'session.webm'` to record a video at the same time.

Measured on the fleet (640×360, quality 60, changing page): Camoufox ~19 fps, Firefox ~12, Chromium ~9 headless / ~5 headful, Chrome ~4, WebKit ~3; 10–24 KB per frame. Chromium-based browsers and WebKit send frames only when pixels change; Firefox and Camoufox keep streaming (~20 fps even on a static page) — throttle in your callback if you only need a preview.

- Needs a Playwright **1.60+** client (not on Rebrowser, which uses 1.52).
- Frames go only to the client that drives the page; relay them yourself to show them elsewhere. View-only.
- No extra cost: frames travel over the session you already pay for.
- With `"cdp": true`, use CDP `Page.startScreencast` over `cdpUrl` ([cdp.md](cdp.md)).

## Watch or take control from outside

Show a session to a person — to watch, or to take over for a captcha or a one-time code — without your code's help. In the [dashboard](https://cdpfleet.com/app): **Watch** / **Take control**. Over the API:

```
POST https://router01.cdpfleet.com/admin/session/live      (x-api-key)
{ "session_id": "…", "mode": "view" | "control" }
→ { "live_url": "wss://router01.cdpfleet.com/session/…/live?token=…", "protocol": "vnc" | "screencast", "expires_in_s": 300, … }
```

- `vnc` (headful sessions, the default): open with noVNC — `new RFB(el, live_url)`, `rfb.viewOnly = mode === 'view'`.
- `screencast` (headless Chromium-based sessions launched with `"live_view": true`): JPEG frames as binary messages; in control mode send `mouse` / `key` / `text` JSON messages in page CSS pixels.
- Links are single-use (5 minutes); at most 4 viewers; watching never ends or bills the session; your code must have connected once first. WebKit ignores clicks from the viewer.
- `"screen_size": "1366x768"` at launch sets a headful session's display; open Playwright contexts with `viewport: null` to see it as `screen` and to make the page fill the whole view (otherwise Playwright sizes the window to the 1280×720 default viewport).

Full page: [cdpfleet.com/docs/live-view](https://cdpfleet.com/docs/live-view).
