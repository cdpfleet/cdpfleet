# Account API

Read your plan, usage, sessions and traffic, and end sessions, over HTTPS. Authenticate with your API key in `x-api-key`; base URL `https://cdpfleet.com`.

| Method | Path | What |
|---|---|---|
| GET | `/v1/me` | Your account, active plan and key list (prefixes only) |
| GET | `/v1/me/usage` | Today's usage: budget, used and remaining thread-seconds, rolling-hour usage, active sessions. `?day=YYYY-MM-DD` for a past day |
| GET | `/v1/me/usage/history` | Daily thread-seconds and session counts; `?days=30` (up to 366) |
| GET | `/v1/me/sessions` | Session history: start, end, duration, browser, threads, end reason. `?from`, `?to` (ISO), `?limit` (up to 1000) |
| GET | `/v1/me/threads` | Your live sessions with running time and deadline |
| DELETE | `/v1/me/threads/{sessionId}` | End one of your live sessions now |
| GET | `/v1/me/bandwidth` | Proxy traffic: totals, per day, per proxy and the top 50 hosts. `?days` 1–31 |
| GET | `/v1/me/bandwidth.csv` | The same as CSV, one row per day, session, host and proxy |
| GET | `/v1/me/sessions/{id}/bandwidth` | One session's traffic per host |
| GET | `/v1/me/proxies` | Your saved proxies with their last check (passwords masked) |
| GET | `/v1/me/proxy-rules` | Your proxy-rule sets |

```bash
curl -s https://cdpfleet.com/v1/me/usage -H "x-api-key: $CDPFLEET_API_KEY"
```

Creating and revoking keys, saving proxies and billing need a dashboard sign-in, not an API key.
