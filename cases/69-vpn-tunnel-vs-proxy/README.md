# VPN Tunnel vs a plain residential proxy: WebRTC, HTTP/3 and two bot-protection pages

**Medium** · 2026-10-24 · Google Chrome, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/vpn-tunnel-vs-proxy)

> The same sticky residential exit with and without `vpn_tunnel`, on headful Chrome and Camoufox: which public IP WebRTC shows, whether HTTP/3 (QUIC) gets used, and what Cloudflare's challenge test page and DataDome's own site make of each.

## The problem

A proxy carries TCP, so a normal proxied session sends no UDP: WebRTC can't reach a STUN server and HTTP/3 (which runs over UDP) never starts. A real home connection does both, and a site can notice the difference. `vpn_tunnel` gives the browser a network of its own whose only way out is the residential exit, UDP included. What actually changes, and does it matter to bot protection?

## What we used, and why

| What | Why |
|---|---|
| `cdpfleet-resi-country-us-session-<id>` | One sticky Tier 1 exit per browser, used for both rows of that browser, so the only difference between them is the tunnel. `vpn_tunnel` needs a Tier 1 or 2 token and isn't combined with `proxy_rules`. |
| `vpn_tunnel: true` | The browser's traffic, UDP included, leaves through the residential exit. |
| WebRTC leak test | ICE candidates against `stun:stun.l.google.com:19302`, as in [case 54](https://cdpfleet.com/docs/cases/webrtc-ip-leak): a `srflx` candidate is the public IP the STUN server saw. |
| `nextHopProtocol` on cloudflare-quic.com | Four loads in fresh tabs: the first is announced over HTTP/2 (`Alt-Svc`); a browser that can use QUIC switches to `h3` on a later one. |
| Two bot-protection pages | Cloudflare's challenge test page from [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through) and DataDome's own site from [case 65](https://cdpfleet.com/docs/cases/datadome-what-gets-through), one load each per row, nothing clicked. |

## How it works

1. For each browser, launch a session on its sticky exit without the tunnel, then one with `vpn_tunnel: true` on the same exit.
2. Read the exit IP over HTTP, run the WebRTC leak test and record every public address it shows.
3. Load cloudflare-quic.com four times in fresh tabs, 2.5 s apart, and record each navigation's protocol.
4. Load scrapingcourse.com's Cloudflare challenge (watched 20 s) and datadome.co (watched 12 s) in fresh contexts and classify each as content, cleared, challenge, blocked or error.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-24):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Network | Exit IP (HTTP) | WebRTC public IP | cloudflare-quic.com, 4 loads | Cloudflare challenge | datadome.co | Measured |
|---|---|---|---|---|---|---|---|
| Google Chrome, headful | residential proxy | 203.0.113.1 | none (mDNS name only) | h2 → h2 → h2 → h2 | challenge | challenge | 2026-10-24 |
| Google Chrome, headful | residential proxy + vpn_tunnel | 203.0.113.1 | 203.0.113.1 (the exit) | h2 → h2 → h3 → h3 | challenge | challenge | 2026-10-24 |
| Camoufox, defaults | residential proxy | 203.0.113.2 | 203.0.113.2 (the exit) | h2 → h2 → h2 → h2 | cleared (9 s) | content | 2026-10-24 |
| Camoufox, defaults | residential proxy + vpn_tunnel | 203.0.113.2 | 203.0.113.2 (the exit) | h2 → h3 → h3 → h3 | cleared (10 s) | content | 2026-10-24 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The tunnel changes what WebRTC shows, never adds a second IP.** On 2026-10-24, across five runs, headful Chrome on a plain residential proxy showed no public address at all (only an anonymous mDNS name, as in [case 54](https://cdpfleet.com/docs/cases/webrtc-ip-leak)); with `vpn_tunnel` on the same exit it showed exactly the exit IP, every time. No row in any run showed an address other than its exit.
- **HTTP/3 only happens through the tunnel.** Without it, all four loads of cloudflare-quic.com used `h2` in every row of every run. With it, Chrome switched to `h3` from the third load and Camoufox from the second: the first load is announced over HTTP/2, and the browser moves to QUIC once it has seen the announcement.
- **An exit that doesn't carry UDP looks like no tunnel at all.** In one of the five runs, Camoufox's tunnel row showed only an mDNS name and never used HTTP/3: that residential exit didn't pass UDP, so WebRTC and QUIC failed quietly, and HTTP kept working. The server's own IP is never used as a fallback. Keep the exit sticky and check WebRTC once if it matters.
- **Camoufox already reports the exit over WebRTC without the tunnel** (its `geoip` default), so for Camoufox the tunnel's visible gain is HTTP/3, and real UDP behind the address it reports.
- **The two bot-protection pages didn't change their verdict for the tunnel.** Chrome stayed on Cloudflare's challenge test page and met DataDome's CAPTCHA in all ten of its rows, tunnel or not; Camoufox got DataDome's site as content in all ten. On Cloudflare's challenge Camoufox passed by itself in all five tunnel rows (in 4–10 s) and in two of five without; five runs aren't enough to credit the tunnel for that. The browser decided, as in [case 64](https://cdpfleet.com/docs/cases/cloudflare-what-gets-through) and [case 65](https://cdpfleet.com/docs/cases/datadome-what-gets-through).
