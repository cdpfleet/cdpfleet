# Swap a proxy mid-session without losing the login — and the keep-alive trap

**Hard** · 2026-09-30 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/live-proxy-swap)

> One API call changes the exit IP of a running browser. Open connections don't follow; storageState moves everything over.

## The problem

A proxy gets blocked or slow halfway through a logged-in flow. Restarting the browser means logging in again. Instead, you can point the running browser at a different proxy with one call — but does traffic really move, and what happens to the connections the browser already has open?

## What we used, and why

| What | Why |
|---|---|
| `proxy_updatable: true` | Kept in the code for clarity: every cdpfleet session is swappable now, whatever you send. |
| `POST https://<router>/admin/session/proxy` | The swap call, sent to the host in your `wsUrl` with your API key: `{session_id, proxy}`. It returns in milliseconds. |
| httpbin.org cookies + `localStorage` | Stand-ins for a login: state that must survive the swap. |
| `api.ipify.org` / `api64.ipify.org` | Two different hosts that echo the exit IP — one we have already connected to, one we haven't. |
| `context.storageState()` | Exports cookies and localStorage so a new context can continue where the old one stopped. |

## How it works

1. Launch with a residential proxy and `proxy_updatable: true`; set cookies and localStorage; note the exit IP.
2. Swap the proxy to a SOCKS5 proxy.
3. Ask the same host again (reused keep-alive connection), then a host we haven't talked to (new connection).
4. Move everything to a new context with `storageState` and check exit IP, cookies and localStorage.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `SOCKS_PROXIES` (see [cases/README.md](../README.md#environment)).

## What we got

| Step | Exit IP | Cookies | localStorage |
|---|---|---|---|
| before the swap | 203.0.113.1 | {"cart":"3-items","login":"alice"} | (set) |
| same tab, reused connection | 203.0.113.1 | — | — |
| same tab, new connection | 203.0.113.2 | — | — |
| new context + storageState | 203.0.113.2 | {"cart":"3-items","login":"alice"} | half-written review |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The swap only applies to new connections.** The same tab asking the same host again still came out of the **old** proxy: the browser reused its open keep-alive connection. A host it hadn't connected to yet came out of the new proxy.
- **To move everything, open a new context with the old `storageState`.** A new context has its own connection pool, so every request goes through the new proxy — and the cookies and localStorage came along.
- **Same browser, same session, no restart:** no relaunch and no re-login, and billing continues on the same session.
- Every session is swappable — single proxies, pools and `proxy_rules` alike; no launch option is needed.
