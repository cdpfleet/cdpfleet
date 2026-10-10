# cdpfleet SDKs and MCP server

Small client libraries for [cdpfleet](https://cdpfleet.com) — launch a real remote browser
(retrying when the fleet is busy), connect **your own** Playwright to it, reconnect
keep_alive sessions, get live-view links, swap proxies, list recordings and usage.

| | package | source |
|---|---|---|
| Node.js | `npm install cdpfleet` | [node/](node) |
| Python | `pip install cdpfleet` | [python/](python) |
| Java | `com.cdpfleet:cdpfleet` | [java/](java) |
| C# | `dotnet add package Cdpfleet` | [csharp/](csharp) |
| Go | `go get github.com/cdpfleet/cdpfleet/sdk/go` | [go/](go) |
| MCP server | `npx -y cdpfleet-mcp` | [mcp/](mcp) |

All five follow one specification: [SPEC.md](SPEC.md). Docs: https://cdpfleet.com/docs/sdk
and https://cdpfleet.com/docs/mcp. The [code builder](https://cdpfleet.com/docs/builder)
writes SDK or plain-Playwright code for any browser and options.
