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


## Opera

Endpoint `POST /opera/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/opera)

Opera, with stable, beta and developer channels.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |
| `channel` | `"stable" \| "beta" \| "dev" \| "developer"` | stable | Release channel of the browser build. Opera calls its dev channel `developer`. `browser.version()` reports the underlying Chromium version; for Brave, Opera, Whale and Yandex that differs from the product version listed here. |


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


## Rebrowser

Endpoint `POST /rebrowser/session` · connect with Playwright `chromium` 1.52 · [docs page](https://cdpfleet.com/docs/browsers/rebrowser)

Chromium launched with Rebrowser's anti-detection patches. **Connect with Playwright 1.52** — this browser's server is pinned to 1.52 and newer clients time out on connect. The generated examples already use 1.52.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |


## CloakBrowser

Endpoint `POST /cloakbrowser/session` · connect with Playwright `chromium` 1.60 · [docs page](https://cdpfleet.com/docs/browsers/cloakbrowser)

CloakBrowser, a Chromium build hardened against fingerprinting.

| Option | Type | Default | What it does |
|---|---|---|---|
| `proxy` **(required)** | `string \| object \| array` | none — required | Upstream proxy for the browser (required). **Required for every session**: a launch without a proxy returns `400 proxy_required`, so your sessions never browse from our servers' own IPs. An unparseable proxy returns 400. |
| `proxy_rules` | `array` | none | Send specific destination hosts through specific proxies. Needs `proxy` to be set as the default route. Rules route through a proxy only; routing a host with no proxy (direct from our servers) is not available. The first matching rule wins — keep rules from overlapping so traffic is attributed to the right one. |
| `inactivity_timeout` | `number \| string` | 60s | End the session after this long with no traffic on its WebSocket. A slow page load through a proxy can produce no traffic for a while, so keep it at 60 s or more. Shared threads can lower it but not raise it above 60 s; dedicated threads can set up to 24 h. Never longer than the session's overall timeout. |
| `overall_timeout` | `number \| string` | your plan's maximum session length (24 h) | Hard cap on the session's total lifetime, regardless of activity. |
| `headless` | `boolean \| "new" \| "shell"` | false (headful, on a virtual display) | Run without a display. |


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
| `webgl_config` | `[vendor, renderer]` | generated | Force a specific WebGL vendor/renderer pair. Requires `os`, and the pair must be valid for that OS or the launch fails. Ignored with `block_webgl`. |
| `ff_version` | `number` | the real Firefox major | Firefox major version to claim in the fingerprint. A reported version that differs from the real engine is detectable. |
| `main_world_eval` | `boolean` | false | Allow scripts in the page's main world. Main-world scripts are visible to the page. |


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

