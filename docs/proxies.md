# Proxies

Every session browses through your proxy. You choose where traffic exits; cdpfleet never browses from its own IPs.

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
