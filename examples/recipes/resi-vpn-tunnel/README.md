# cdpfleet residential with VPN Tunnel: WebRTC and HTTP/3 on the same exit

A sticky Tier 1 residential exit plus `"vpn_tunnel": true`: the browser's UDP leaves through the same exit as its HTTP, so WebRTC shows the exit IP instead of nothing, and HTTP/3 works. Measured on prod with this launch: the WebRTC leak test showed exactly one public address, the exit IP, and cloudflare-quic.com went h2, h2, h3, h3 over four loads (a browser switches to HTTP/3 once a page has announced it). Needs a Tier 1 or 2 token; keep it sticky, and don't combine the tunnel with `proxy_rules`. See [VPN Tunnel](https://cdpfleet.com/docs/proxies#vpn-tunnel) and [case 69](https://cdpfleet.com/docs/cases/vpn-tunnel-vs-proxy).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
