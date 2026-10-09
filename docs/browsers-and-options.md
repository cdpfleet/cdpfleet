# Browsers and launch options

Generated from the same option table as the [code builder](https://cdpfleet.com/docs/builder). Every launch is `POST https://starter.cdpfleet.com/<endpoint>/session` with a JSON body of these options; `proxy` is required.

| Browser | Endpoint | Playwright client | Client version |
|---|---|---|---|
| Google Chrome | `/chrome/session` | chromium | 1.60 |
| Microsoft Edge | `/edge/session` | chromium | 1.60 |
| Brave | `/brave/session` | chromium | 1.60 |
| Opera | `/opera/session` | chromium | 1.60 |
| Naver Whale | `/whale/session` | chromium | 1.60 |
| Yandex Browser | `/yandex/session` | chromium | 1.60 |
| Chromium | `/chromium/session` | chromium | 1.60 |
| Patchright | `/patchright/session` | chromium | 1.60 |
| Rebrowser | `/rebrowser/session` | chromium | 1.52 |
| CloakBrowser | `/cloakbrowser/session` | chromium | 1.60 |
| Camoufox | `/camoufox/session` | firefox | 1.60 |
| Firefox | `/firefox/session` | firefox | 1.60 |
| WebKit | `/webkit/session` | webkit | 1.60 |


## Google Chrome

Endpoint `POST /chrome/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/chrome)

The real Google Chrome, with stable, beta and dev channels and recent previous majors. The best choice when a site must see genuine Chrome.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `version` | `string` | none (latest stable) | Pin an older major version. Previous majors rotate out automatically: Chrome keeps its two most recent previous majors and drops the oldest within about a day of a new stable release; Camoufox keeps recent majors plus long-term ones. Read the live list rather than hard-coding a number. Send either `version` or a pre-release `channel`, not both: together they return `400` (`channel: "stable"` with a `version` is fine). |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Microsoft Edge

Endpoint `POST /edge/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/edge)

Microsoft Edge on Linux, with stable, beta and dev channels.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Brave

Endpoint `POST /brave/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/brave)

The Brave browser, with stable and beta channels.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Opera

Endpoint `POST /opera/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/opera)

Opera, with stable, beta and developer channels. Headless over CDP (`cdpUrl`), open pages in a new context (`browser.newContext()`): Opera doesn't render pages in its default context there — so `keep_alive` over CDP, which only keeps the default context, needs headful Opera or `wsUrl`.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Naver Whale

Endpoint `POST /whale/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/whale)

Naver Whale, the browser that ships with Naver services in Korea. Stable channel.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Yandex Browser

Endpoint `POST /yandex/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/yandex)

Yandex Browser. Stable channel.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Chromium

Endpoint `POST /chromium/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/chromium)

The open-source Chromium build that ships with Playwright. Fast, predictable and the most compatible choice for automation.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Patchright

Endpoint `POST /patchright/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/patchright)

Chromium launched with Patchright's anti-detection patches (leaner automation flags, no Runtime.enable leak). The patches apply on our side, so connect with ordinary Playwright.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Rebrowser

Endpoint `POST /rebrowser/session` · connect with Playwright `chromium` 1.52 · [docs page](https://cdpfleet.com/docs/browsers/rebrowser)

Chromium launched with Rebrowser's anti-detection patches. **Connect with Playwright 1.52** — this browser's server is pinned to 1.52 and newer clients time out on connect. The generated examples already use 1.52. `keep_alive` isn't available: the 1.52 server can't keep contexts across connections, so a launch with it returns 400.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## CloakBrowser

Endpoint `POST /cloakbrowser/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/cloakbrowser)

CloakBrowser, a Chromium build hardened against fingerprinting. Pages see its fingerprint's screen (1920×1080) whatever the display; `screen_size` only sizes the display (live view, recordings).

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `cdp` | `boolean` | false | Also expose a Chrome DevTools Protocol endpoint (cdpUrl). Same session, proxy and limits. You can connect to `wsUrl` and `cdpUrl` at once, but closing either ends the session. Not on Patchright and Rebrowser (their stealth lives in the Playwright layer CDP bypasses), Whale, Camoufox, Firefox or WebKit. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `live_view` | `boolean` | false (headful sessions are always watchable) | Make a headless session watchable and controllable from outside. Headless Chromium-based browsers only (Camoufox, Firefox and WebKit are watchable when headful, their default). Doesn't give your code CDP access — add `"cdp": true` for that. The session is also [recorded](https://cdpfleet.com/docs/recordings) unless you add `"record": false`. See [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Camoufox

Endpoint `POST /camoufox/session` · connect with Playwright `firefox` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/camoufox)

Camoufox is a Firefox build that spoofs a complete, consistent fingerprint at the engine level. Set its identity with launch options (below) rather than with client-side `newContext` settings, which would fight the injected identity. **A `proxy` is required**, and by default the identity is matched to the proxy's location. `POST /session` is an alias for this browser.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "virtual"` | false (headful, on a virtual display) | Run without a display. |
| `version` | `string` | none (latest stable) | Pin an older major version. Previous majors rotate out automatically: Chrome keeps its two most recent previous majors and drops the oldest within about a day of a new stable release; Camoufox keeps recent majors plus long-term ones. Read the live list rather than hard-coding a number. Send either `version` or a pre-release `channel`, not both: together they return `400` (`channel: "stable"` with a `version` is fine). |
| `os` | `"windows" \| "macos" \| "linux" \| array` | random among windows / macos / linux | Operating system of the generated fingerprint. Required when `webgl_config` is set. Any other value returns 400. |
| `locale` | `string \| array` | derived from the proxy's exit IP | Browser locale(s). An unknown locale fails the launch. |
| `geoip` | `boolean` | true | Match timezone, locale, geolocation and WebRTC IP to the proxy's exit IP. If the proxy can't reach the IP lookup services, the launch fails. |
| `humanize` | `boolean \| number` | true | Human-like cursor movement. |
| `block_images` | `boolean` | false | Block all images. Blocked images are themselves a detectable signal. |
| `block_webrtc` | `boolean` | false | Disable WebRTC entirely. |
| `block_webgl` | `boolean` | false | Disable WebGL. A missing WebGL is itself a fingerprint signal. |
| `disable_coop` | `boolean` | false | Disable Cross-Origin-Opener-Policy. Detectable. |
| `screen` | `object` | none | Constrain the fingerprint's screen size. Ignored when `fingerprint` is given. |
| `window` | `[width, height]` | random | Fixed outer window size. Ignored when `fingerprint` is given. |
| `fingerprint` | `object` | generated | Use a complete BrowserForge fingerprint. A non-Firefox fingerprint returns 400. |
| `config` | `object` | none | Override individual fingerprint properties. Strictly validated: an unknown property (e.g. Chromium-only `navigator.deviceMemory`) or a wrong value type returns 400. Keep overrides consistent with `os` (for example, touch points stay 0 on a desktop OS). |
| `fonts` | `array` | the target OS's fonts | Extra font families to expose. |
| `custom_fonts_only` | `boolean` | false | Expose only the fonts in `fonts`. Requires `fonts`. |
| `webgl_config` | `[vendor, renderer]` | generated | Force a specific WebGL vendor/renderer pair. Requires `os`, and the pair must be valid for that OS: otherwise the launch returns 400 with the valid pairs. Ignored with `block_webgl`. |
| `ff_version` | `number` | the real Firefox major | Firefox major version to claim in the fingerprint. A reported version that differs from the real engine is detectable. |
| `main_world_eval` | `boolean` | false | Allow scripts in the page's main world. Main-world scripts are visible to the page. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## Firefox

Endpoint `POST /firefox/session` · connect with Playwright `firefox` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/firefox)

The Firefox build that ships with Playwright.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean` | false (headful, on a virtual display) | Run without a display. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |


## WebKit

Endpoint `POST /webkit/session` · connect with Playwright `webkit` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/webkit)

WebKit, the engine behind Safari, as built by Playwright. Combine it with Playwright's iPhone device descriptors for mobile Safari.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean` | false (headful, on a virtual display) | Run without a display. |
| `screencast` | `boolean` | false | Stream the page to your code (Playwright page.screencast). View-only and only for the client that drives the page. With `"cdp": true`, use CDP `Page.startScreencast` instead. To watch from somewhere else — or take control — see `live_view` and [Live view](https://cdpfleet.com/docs/live-view). |
| `screen_size` | `"WxH" \| { width, height }` | the server's display (1280×720 window) | Resolution of the session's own display (headful only). Headful sessions only — a headless launch with `screen_size` returns 400. Playwright contexts report a screen equal to their viewport (1280×720) unless you open them with `viewport: null` (Python `no_viewport=True`, Java `setViewportSize(null)`, C# `ViewportSize = ViewportSize.NoViewport`, Go `NoViewport: playwright.Bool(true)`); CloakBrowser always reports its fingerprint's 1920×1080; Camoufox always reports the display — the nearest real-world screen size no larger than it (exact for common sizes such as 1366×768 or 1920×1080); with `"os": "macos"` use 1440×900 or larger, since no Mac screen is smaller (400 otherwise). |
| `keep_alive` | `boolean` | false | Keep the session when your client disconnects (dedicated threads only). Not on Rebrowser. Dedicated threads only (`403 keep_alive_requires_dedicated` on shared plans). The session is billed while it's alive, attached or not — and `browser.close()` only disconnects: the session ends after `inactivity_timeout` without a client, or at once with `DELETE /v1/me/threads/{sessionId}` (or End in the dashboard). Set `inactivity_timeout` to how long it may wait for you. |
| `record` | `boolean` | true (when recording applies) | Opt this session out of recording with false. Recording applies to headful sessions and to headless ones launched with `"cdp": true` or `"live_view": true`, at 480p (720p or 1080p on a recording plan). Recordings appear on the dashboard's Sessions page. See [Session recording](https://cdpfleet.com/docs/recordings). |

