# Sessions and limits

## Threads

Your plan gives you a number of **threads** — concurrent capacity. A session takes threads by type:

| Session type | Threads |
|---|---|
| Linux headless | 1 |
| Linux headful (a virtual display — the default) | 2 |
| Mobile WebView — Android WebView on hardware phones, iPhone WKWebView on Apple's iOS Simulator (hardware iPhone coming soon); early access | 3 |
| Mobile Chrome — Chrome and other full browsers on hardware Android phones; early access | 4 |

A launch that would exceed your threads gets `429 threads_exceeded` until a session closes.

## Plans

- **Community (free)** — 2 threads, 60 browser-minutes per thread per day, sessions up to 15 minutes, every browser, an included shared proxy (slower at peak), lowest priority. No recordings, persistent profiles or `keep_alive`. Invite-only for now; activated by verifying a card that is never charged. Request data may be used to learn which browser setups work on which sites (Terms, section 4A).
- **Shared threads** — pooled capacity: each purchased thread contributes browser-minutes per day to your account pool (60, 120, 240 or 480 by product); at most 25 % of the daily pool in any rolling 60 minutes. Idle timeout up to 60 s. Sessions hold 15-minute leases that renew automatically while shared capacity allows.
- **Dedicated threads** — reserved capacity for continuous use: no daily or hourly limit, no leases; idle timeout up to 24 h.

Prices and sizes: [cdpfleet.com/docs/pricing](https://cdpfleet.com/docs/pricing).

Minutes are counted per thread: a headful session takes 2 threads, so it uses two threads' minutes per minute.

## Metering

- Usage is **thread-seconds**: threads × seconds, nothing rounded up.
- The clock starts when your browser is ready (the launch response) and stops when either side closes the WebSocket.
- A session you never connect to ends after 60 seconds and is metered until then — except one launched with `start_url`, which runs without a client until `inactivity_timeout` ([live-view.md](live-view.md#launch-interactive)). If our server doesn't accept your connection, the session isn't billed.
- The shared daily pool resets at midnight UTC.

## Timeouts

- `inactivity_timeout` — end the session after this long with no WebSocket traffic (default 60 s; shared plans can only lower it).
- `overall_timeout` — hard cap on the session's lifetime, at most 24 h (15 minutes on Community; on shared plans also your remaining daily minutes).

Values are seconds (`90`) or strings with a unit (`500ms`, `45s`, `10m`, `2h`).
