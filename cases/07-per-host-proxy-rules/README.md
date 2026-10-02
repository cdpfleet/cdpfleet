# Different sites, different proxies, one browser: proxy_rules

**Medium** · 2026-09-30 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/per-host-proxy-rules)

> Route one host through a SOCKS5 proxy, another through a round-robin pool and everything else through residential — plus the hosts a rule can't move.

## The problem

Residential bandwidth is expensive; datacenter proxies are cheap but get blocked on the sites that matter. A common pattern: send the protected site through residential, static assets or APIs through cheap proxies. `proxy_rules` does this inside one browser — let's check each rule really exits where it should.

## What we used, and why

| What | Why |
|---|---|
| `proxy` | The default route: every host no rule matches (residential here). |
| `proxy_rules` rule 1 | `*.ident.me` and `ident.me` → one SOCKS5 proxy. Host patterns take `*` wildcards. |
| `proxy_rules` rule 2 | `httpbin.org` → a list of two proxies, used round-robin per connection. |
| `proxy_rules` rule 3 | `api.ipify.org` → a SOCKS5 proxy. It won't apply, on purpose — see below. |
| A new context per reading | Each context has its own connection pool, so every reading is a new connection and the round-robin shows. |

## How it works

1. Launch with a default proxy and three rules.
2. Read the exit IP from each echo service, each time in a fresh context (one retry if a proxy drops the connection).
3. Compare each exit with the proxy its rule names.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `SOCKS_PROXIES` (see [cases/README.md](../README.md#environment)).

## What we got

| URL | Exit IP | Rule |
|---|---|---|
| https://v4.ident.me/ | 203.0.113.1 | 203.0.113.1 |
| https://httpbin.org/ip | 203.0.113.2 | 203.0.113.2 or 203.0.113.3 |
| https://httpbin.org/ip | 203.0.113.3 | 203.0.113.2 or 203.0.113.3 |
| https://httpbin.org/ip | 203.0.113.2 | 203.0.113.2 or 203.0.113.3 |
| https://api.ipify.org/ | 203.0.113.4 | default proxy (IP-lookup host) |
| https://www.cloudflare.com/cdn-cgi/trace | 203.0.113.5 | default proxy |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **`ident.me` exited through its SOCKS5 proxy, `httpbin.org` alternated between its two, and everything else went out residential.**
- **Round-robin is per connection,** not per request — requests on an open keep-alive connection stay on its proxy. A pool member that fails is skipped, so a run can show the same proxy twice.
- **IP-lookup services always use the default proxy:** `api.ipify.org`, `checkip.amazonaws.com`, `ipinfo.io`, `icanhazip.com`, `ifconfig.co`, `ipecho.net`. Camoufox's `geoip` uses them to match its location to the exit, so they must follow `proxy`. To check a rule, use another echo service such as `v4.ident.me` or `httpbin.org/ip`.
- Rules route through a proxy only; there's no "direct from cdpfleet" rule — every session browses from a proxy you choose.
