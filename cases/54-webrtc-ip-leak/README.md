# The WebRTC IP leak test behind a proxy: what Chromium, Chrome, Firefox and Camoufox reveal

**Medium** · 2026-10-16 · Chromium, Google Chrome, Firefox, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/webrtc-ip-leak)

> Run the leak test fingerprinting sites use — ICE candidates against a public STUN server — on five setups behind sticky residential exits. No setup exposes a second public IP: the Chromium builds and Firefox show only an anonymous mDNS name, Camoufox reports the proxy's exit, and `block_webrtc` removes the API.

## The problem

A proxy carries the browser's HTTP traffic, but WebRTC opens its own UDP connections — and a page can ask a public STUN server "which address do you see me at?" without any permission prompt. On a local browser behind a proxy or VPN, that answer is often your real IP: the classic WebRTC leak, and a standard check on fingerprinting sites. What does a page learn on the fleet, browser by browser?

## What we used, and why

| What | Why |
|---|---|
| `RTCPeerConnection` + `stun:stun.l.google.com:19302` | The standard leak test: create a data channel, make an offer, collect ICE candidates. A `srflx` candidate is the public IP the STUN server saw; `host` candidates are local addresses (browsers hide them behind random `.local` names). |
| `https://api.ipify.org` | The exit IP the site sees over HTTP, through the proxy — what any WebRTC address is compared with. |
| `cdpfleet-resi-country-us-session-…` | A sticky residential exit per setup: one IP for the whole session, so "the same IP as the exit" is a fair comparison. |
| `block_webrtc: true` (Camoufox) | Turns WebRTC off in the engine. |
| `geoip` (Camoufox, on by default) | Matches location to the proxy — and makes WebRTC report the proxy's exit IP. |

## How it works

1. For each setup, launch a session with its own sticky exit and read the exit IP from ipify.
2. Run the leak test in the page and parse every ICE candidate: its address and type.
3. Compare any public address with the exit IP: the same, different (a leak) or none.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-16):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | WebRTC API | Candidates | mDNS host | Public IP via WebRTC | Exit IP (HTTP) | Verdict |
|---|---|---|---|---|---|---|
| Chromium | yes | 1 | 1 | — | 203.0.113.1 | nothing exposed |
| Google Chrome | yes | 1 | 1 | — | 203.0.113.2 | nothing exposed |
| Firefox | yes | 2 | 2 | — | 203.0.113.3 | nothing exposed |
| Camoufox (defaults: geoip on) | yes | 3 | 2 | 203.0.113.4 | 203.0.113.4 | same as the exit |
| Camoufox, block_webrtc: true | no | 0 | 0 | — | 203.0.113.5 | nothing exposed |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **No setup leaked a second IP.** On cdpfleet a session that uses a proxy can't send UDP past it, so the STUN request never leaves and there's no `srflx` candidate with the server's own address.
- **Chromium, Chrome and Firefox keep WebRTC but expose nothing:** `RTCPeerConnection` works and gathering completes with only `.local` host candidates — random mDNS names, not addresses. That's what a real visitor on a network that blocks UDP looks like.
- **Camoufox reports the proxy's exit instead:** with its defaults (`geoip` on) the only public address in the candidates was the exit IP, so WebRTC tells the same story as HTTP. Keep the proxy sticky: the address is looked up when the browser starts, and a rotating proxy may exit elsewhere later.
- **`block_webrtc: true` removes the API** (`RTCPeerConnection` is undefined). Nothing leaks, but a browser without WebRTC at all is unusual — keep the default unless a site or your own policy needs it gone.
- Running the same test on your own machine behind a proxy is worth it: the `srflx` candidate there is often your real IP, unless the browser's WebRTC IP-handling policy or an extension stops it.
