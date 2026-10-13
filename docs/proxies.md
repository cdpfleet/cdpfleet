# Proxies

Every session browses through a proxy — yours, or the cdpfleet residential proxy. cdpfleet never browses from its own server IPs.

## cdpfleet residential proxy

No proxy of your own? Send a `cdpfleet-resi` token as `proxy`. Traffic (up + down, 1 GB = 10⁹ bytes) is billed at the token's price tier from a prepaid USD balance topped up in the [dashboard](https://cdpfleet.com/app#proxies), separately from threads. Every account starts with **$5 free credit**.

| Tier | Token | Price per GB |
|---|---|---|
| Tier 1 | `cdpfleet-resi…` (no tier) or `cdpfleet-resi-tier-1…` | $5.00 |
| Tier 2 | `cdpfleet-resi-tier-2…` | $2.50 |
| Tier 3 | `cdpfleet-resi-tier-3…` | $1.00 |
| Tier 4 | `cdpfleet-resi-tier-4…` | $0.50 |

Use Tier 1 for the pages that matter and a cheaper tier for bulk traffic such as CDN assets; the pools behind each tier can differ and may change, the targeting options are the same.

```jsonc
"proxy": "cdpfleet-resi"                                        // Tier 1: new residential IP per connection, any country
"proxy": "cdpfleet-resi-country-us"                             // exits in the United States
"proxy": "cdpfleet-resi-country-us-os-windows"                  // … from Windows devices
"proxy": "cdpfleet-resi-country-de-session-cart1-lifetime-30"   // the same German IP for up to 30 minutes
"proxy": "cdpfleet-resi-tier-4-country-us"                      // Tier 4 ($0.50/GB), US exits
```

Mix tiers in one session with `proxy_rules` — pages on Tier 1, CDN assets on Tier 4:

```json
{
  "proxy": "cdpfleet-resi-country-us",
  "proxy_rules": [
    { "hosts": ["*.cloudfront.net", "*.akamaized.net", "static.*"], "proxy": "cdpfleet-resi-tier-4-country-us" }
  ]
}
```

| Part | Values | Meaning |
|---|---|---|
| `-tier-N` | `1`–`4` | Price tier; Tier 1 if omitted |
| `-country-XX` | ISO two-letter code | Exit country; any if omitted |
| `-os-…` | `windows`, `android`, `ios`, `mac` | Device type behind the residential IP |
| `-session-ID` | 1–10 of `A-Z a-z 0-9 _` | Sticky: same IP for every connection; reuse the name later to keep it |
| `-lifetime-MIN` | 1–1440, only with a session | How long the sticky IP is kept (default 30) |

- Parts go in this order; otherwise `400 invalid_proxy` names the wrong part.
- Use it as the whole `proxy` or as one rule's proxy in `proxy_rules` — not inside a list of proxies.
- Empty balance → `402 proxy_balance_exhausted`. Metering runs about once a minute; a running session may go $0.50 past zero before it's ended.
- Balance: `GET /v1/me/proxy-balance` ([Account API](account-api.md)).

## Formats

```jsonc
"proxy": "http://user:pass@host:8080"          // HTTP or HTTPS proxy
"proxy": "socks5://user:pass@host:1080"        // SOCKS5, with or without auth
"proxy": "host:8080"                           // no scheme = http://
"proxy": { "server": "http://host:8080", "username": "user", "password": "pass" }
```

## Pools

A list of proxies is used round-robin, per connection:

```jsonc
"proxy": ["http://u:p@res1.example:8080", "http://u:p@res2.example:8080", "socks5://u:p@res3.example:1080"]
```

## Per-host rules

`proxy_rules` sends specific hosts through other proxies; everything else uses `proxy`. Host patterns take `*` wildcards; the first matching rule wins. The optional `name` labels the rule's traffic on your dashboard's Proxy traffic page and CSV export.

```json
{
  "proxy": "http://u:p@residential.example:8080",
  "proxy_rules": [
    { "name": "dc-assets", "hosts": ["*.cloudfront.net", "static.*"], "proxy": ["http://u:p@dc1.example:4444", "http://u:p@dc2.example:4444"] },
    { "name": "api", "hosts": ["api.example.com"], "proxy": "http://u:p@special.example:8080" }
  ]
}
```

A few IP-lookup services — `api.ipify.org`, `checkip.amazonaws.com`, `ipinfo.io`, `icanhazip.com`, `ifconfig.co`, `ipecho.net` — always use the default `proxy` (Camoufox matches its location with them). To check a rule's exit IP, use another echo service such as `https://v4.ident.me`. See the [per-host proxy rules case](../cases/07-per-host-proxy-rules).

## Live swap

Change a running session's proxy without restarting the browser:

```
POST https://<host of your wsUrl>/admin/session/proxy
{ "session_id": "…", "proxy": "http://u:p@new-exit.example:8080" }
```

Open keep-alive connections stay on the old proxy; new connections use the new one. To move everything, open a new browser context with the old `storageState` — see the [live proxy swap case](../cases/06-live-proxy-swap).

## Traffic per host

Your dashboard's **Proxy traffic** page (and the [Account API](account-api.md)) shows bytes per destination host and per proxy/rule, with CSV export.
