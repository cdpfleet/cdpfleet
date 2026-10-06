# Errors

Errors are JSON: `{"error": "…"}`, sometimes with extra fields.

## Launch

| Status | `error` | Meaning | What to do |
|---|---|---|---|
| 400 | `invalid_json` | The body isn't a JSON object | Fix the request |
| 400 | `proxy_required` | The launch has no `proxy` | Add your proxy |
| 400 | `invalid_proxy` | A `cdpfleet-resi` token that doesn't match the grammar, or inside a proxy list | Fix the token ([proxies](proxies.md#cdpfleet-residential-proxy)) |
| 400 | — | A launch option was rejected (the message says which) | Fix the option; don't retry as is |
| 400 | `cdp is not supported on this engine` | `"cdp": true` on a browser without CDP | Use a supported browser or `wsUrl` ([cdp.md](cdp.md)) |
| 400 | `invalid_cdp` | `cdp` isn't a boolean | Send `true` or `false` |
| 401 | `missing_api_key` / `invalid_api_key` | No key, or an unknown or revoked key | Check `x-api-key` |
| 402 | `subscription_inactive` | No active plan | Check your plan in the dashboard |
| 402 | `proxy_balance_exhausted` | The launch uses the cdpfleet residential proxy and the balance is used up | Top up in the dashboard → Proxies |
| 403 | `engine_not_allowed` | Your plan doesn't include this browser | Use another browser |
| 404 | `not_found` | Unknown endpoint, e.g. a misspelled browser | Check the path |
| 429 | `rate_limited` | Too many launches per second/minute | Wait `Retry-After` seconds |
| 429 | `threads_exceeded` | The launch would exceed your threads | Close a session or wait; `Retry-After: 1` |
| 429 | `daily_quota_exhausted` | Shared plans: today's pool is used up | `Retry-After` counts down to midnight UTC |
| 429 | `hourly_quota_exhausted` | Shared plans: 25 % of the daily pool used in the last 60 minutes | Wait; `Retry-After: 60` |
| 500 | — | The browser failed to start (e.g. the proxy is unreachable) | Check the proxy, then retry |
| 503 | `shared_capacity_full` | Shared plans: the shared fleet is full right now | Retry after `Retry-After` (5 s) |
| 503 | `fleet_unavailable` / `starting_up` / `route_unavailable` | The fleet is momentarily busy | Retry with backoff (`Retry-After: 5`) |
| 503 | `resi_unavailable` | The residential proxy is momentarily unavailable on that server | Retry |
| 503 | `version_unavailable` | The requested `version` isn't available; `available` lists the majors | Pick one from `available` |

Retry `503` with backoff and respect `Retry-After` on `429`; don't retry other `4xx` — the request needs fixing. See the [worker-pool case](../cases/10-worker-pool) for a complete retry loop.

## Connecting

- `401` / `403` — wrong key: only the key that launched the session may connect.
- `404` — the session has ended, or the 60-second connect window passed.
- `409` — the session already has a connection.
- `502 {"error":"upstream_unreachable"}` — the server running the browser didn't answer (rare). Launch a new session rather than reconnecting; the failed one isn't billed.
