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

Full page: [cdpfleet.com/docs/live-view](https://cdpfleet.com/docs/live-view).
