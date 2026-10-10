# cdpfleet SDK specification (all languages)

One small API, the same shape in Node.js, Python, Java, C# and Go. The SDKs wrap the
public HTTP API; they never bundle Playwright — the caller passes their own Playwright
browser type (`chromium`, `firefox`, `webkit`) so they keep control of the client version
(Rebrowser needs Playwright 1.52, everything else 1.60). No dependencies beyond the
standard library (Java: Gson, which Playwright for Java already depends on).

## Client: `Cdpfleet`

Constructor options (all optional):
| option | default | |
|---|---|---|
| `apiKey` | env `CDPFLEET_API_KEY` | error if neither is set |
| `starterUrl` | `https://starter.cdpfleet.com` | launches |
| `apiUrl` | `https://cdpfleet.com` | account API |
| `maxWait` | 120 s | how long `launch` keeps retrying |

Methods:
- `launch(browser, options) → Session` — `POST {starterUrl}/{browser}/session` with header
  `x-api-key` and JSON body = `options` (passed through untouched, e.g. `proxy`, `headless`,
  `cdp`, `keep_alive`, `record`, `proxy_rules`, Camoufox options). Retries until `maxWait` on
  network errors, 503, and 429 except `daily_quota_exhausted` / `hourly_quota_exhausted`; waits
  `Retry-After` seconds when given, else 1, 2, 4, 8, 10, 10 … s (+ up to 0.5 s jitter). Any other
  non-200 → `CdpfleetError` with `status`, `code` (body `error`), `message`, `body`.
- Account API (`GET` with `x-api-key` on `apiUrl`, returning parsed JSON):
  `me()` → `/v1/me`, `usage()` → `/v1/me/usage`, `threads()` → `/v1/me/threads`,
  `sessions(days?, limit?)` → `/v1/me/sessions?days=&limit=`, `recordings()` → `/v1/me/recordings`,
  `recordingUrl(id)` → `/v1/me/recordings/{id}/url` (returns the `url` string),
  `deleteRecording(id)` → `DELETE /v1/me/recordings/{id}`, `proxyBalance()` → `/v1/me/proxy-balance`.
- `endSession(sessionId)` → `DELETE {apiUrl}/v1/me/threads/{id}`; 404 is not an error (already ended).

## `Session`

Fields: `id` (`sessionId`), `wsUrl`, `cdpUrl` (null unless launched with `cdp: true`),
`browserName` (`browser`), `resourceClass`, `weight`, `expiresAt`, `keepAlive` (from the
launch options), `raw` (the full launch response).

Methods:
- `connect(browserType, options?) → Browser` — `browserType.connect(wsUrl, { headers: { 'x-api-key': key }, ...options })`.
  Remembers the browser. Call again after a disconnect to re-attach to a `keep_alive` session.
  If the connect fails with an error mentioning `session_already_connected` (the router still
  counts the old connection right after a disconnect), retry every 1 s for up to 10 s.
- `connectOverCDP(browserType, options?) → Browser` — the same with `connectOverCDP(cdpUrl, …)`;
  error if `cdpUrl` is null.
- `liveLink(mode = 'view') → object` — `POST https://{router host of wsUrl}/admin/session/live`
  body `{session_id, mode}` (mode `view` | `control`); returns the JSON (`live_url`, `protocol`, …).
- `swapProxy(proxy, proxyRules?) → object` — `POST https://{router host}/admin/session/proxy`
  body `{session_id, proxy, proxy_rules?}` (a swap replaces the whole route: pass the rules again).
- `end()` — `endSession(id)`.
- `close()` — closes every browser this session connected (ignoring errors); then, if the
  session was launched with `keep_alive: true`, calls `end()` (closing only disconnects those).
  Idempotent. Languages with scoped disposal (`using`, `with`, try-with-resources, `defer`) make
  `Session` disposable via `close()`.

## Naming per language

| | Node.js | Python | Java | C# | Go |
|---|---|---|---|---|---|
| package | `cdpfleet` (npm, ESM + .d.ts) | `cdpfleet` (PyPI); `Cdpfleet` sync, `AsyncCdpfleet` async | `com.cdpfleet:cdpfleet`, package `com.cdpfleet` | `Cdpfleet` (NuGet), namespace `Cdpfleet` | `github.com/cdpfleet/cdpfleet/sdk/go`, package `cdpfleet` |
| options | plain object | keyword args or a dict | `Map<String, Object>` | `object` (anonymous object or dictionary) | `cdpfleet.Options` (`map[string]any`) |
| style | `await fleet.launch('chrome', {...})` | `fleet.launch("chrome", proxy=..., headless=False)` | `fleet.launch("chrome", Map.of(...))` | `await fleet.LaunchAsync("chrome", new {...})` | `fleet.Launch("chrome", cdpfleet.Options{...})` |
| disposal | `await session.close()` | `with fleet.launch(...) as session:` | `try (Session s = ...)` | `await using var session = ...` | `defer session.Close()` |

User-Agent header for SDK HTTP calls: `cdpfleet-sdk-<lang>/<version>`. Version 0.1.0.
