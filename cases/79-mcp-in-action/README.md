# MCP in action: an MCP client drives a cloud browser through cdpfleet.com/mcp

**Easy** · 2026-10-31 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/mcp-in-action)

> A script connects to the hosted MCP server with its API key and does what an AI assistant does: list the tools, launch a browser, open a page, read it as an accessibility snapshot, extract data and close the browser — once in one MCP session, and once with a new MCP session for every call, as ChatGPT does. In five languages, four of them on the official MCP SDK.

## The problem

Claude, ChatGPT, Gemini and Cursor reach cdpfleet through its hosted MCP server, `https://cdpfleet.com/mcp`. What they actually do there is a handful of JSON-RPC calls: initialize, list the tools, call them one by one. Doing the same from a script shows what each call costs, what comes back, and what happens to the browser when the client opens a new MCP session for every call — which is how ChatGPT talks to MCP servers.

## What we used, and why

| What | Why |
|---|---|
| `https://cdpfleet.com/mcp` | The hosted MCP server, over Streamable HTTP. Assistants sign in with OAuth; a script sends its API key as `Authorization: Bearer <key>`. |
| The official MCP SDKs | TypeScript `@modelcontextprotocol/sdk` 1.32.2, Python `mcp` 2.3.0, Java `io.modelcontextprotocol.sdk:mcp` 2.1.0 and C# `ModelContextProtocol` 2.3.0. The Go program speaks the protocol with the standard library (JSON-RPC 2.0 over HTTP), because the official Go SDK needs Go 1.23 or newer. |
| `launch_browser` | Headless Chromium through your own proxy (`proxy`), without a recording (`record: false`). One browser at a time per API key. |
| `navigate`, `snapshot`, `evaluate` | Open the catalogue, read it as an accessibility snapshot (the whole page and just the product list), and run an extraction function in the page. |
| `close_browser`, `account` | End the browser session, then check the plan's live sessions. |
| A new MCP session per call | The second run initializes a fresh MCP session for each tool call and drops it without a DELETE, the way ChatGPT does. |

## How it works

1. Connect: initialize an MCP session with the API key as bearer token and list the tools.
2. Launch headless Chromium, open books.toscrape.com, take an accessibility snapshot of the page and of its product list, read the HTML length, and extract every book with `evaluate`.
3. Close the browser, try one more `navigate`, and read `account`; end the MCP session.
4. Then do it again with a new MCP session for every call: launch, open page 2, read where the browser is, close.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-31):

- [Node.js](node.mjs) — npm install @modelcontextprotocol/sdk@1.32.2 && node node.mjs
- [Python](main.py) — pip install mcp==2.3.0 && python main.py
- [Java](Main.java) — Maven with io.modelcontextprotocol.sdk:mcp:2.1.0 and com.google.code.gson:gson:2.11.0 (the templates/java pom plus the MCP SDK), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package ModelContextProtocol --version 2.3.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go run . (standard library only: JSON-RPC over HTTP)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| MCP session | Call | Time | Result | Measured |
|---|---|---|---|---|
| one for the whole run | initialize | 261 ms | session id issued | 2026-10-31 |
| one for the whole run | tools/list | 46 ms | 13 tools: account, click, close_browser, evaluate, launch_browser, list_browsers, live_view, navigate, press, screenshot, snapshot, type, wait_for | 2026-10-31 |
| one for the whole run | launch_browser | 584 ms | chromium 148.0.7778.96, linux_headless, 1 thread | 2026-10-31 |
| one for the whole run | navigate | 3,317 ms | 200 https://books.toscrape.com/ | 2026-10-31 |
| one for the whole run | snapshot | 190 ms | 16,662 characters (whole page) | 2026-10-31 |
| one for the whole run | snapshot | 121 ms | 9,955 characters (the product list) | 2026-10-31 |
| one for the whole run | evaluate | 77 ms | HTML: 50,989 characters | 2026-10-31 |
| one for the whole run | evaluate | 65 ms | 20 books, first: A Light in the Attic (£51.77) | 2026-10-31 |
| one for the whole run | close_browser | 29 ms | session ended | 2026-10-31 |
| one for the whole run | navigate | 25 ms | error: No browser is open. Call launch_browser first. | 2026-10-31 |
| one for the whole run | account | 107 ms | 0 live sessions | 2026-10-31 |
| new for each call (1 of 4, 4 distinct) | launch_browser | 486 ms (541 ms with initialize) | browser session 87f73103… | 2026-10-31 |
| new for each call (2 of 4, 4 distinct) | navigate | 4,413 ms (4,480 ms with initialize) | 200 https://books.toscrape.com/catalogue/page-2.html | 2026-10-31 |
| new for each call (3 of 4, 4 distinct) | evaluate | 36 ms (137 ms with initialize) | 20 books at /catalogue/page-2.html | 2026-10-31 |
| new for each call (4 of 4, 4 distinct) | close_browser | 31 ms (91 ms with initialize) | the same browser session ended | 2026-10-31 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **An MCP call costs tens of milliseconds; the page load is the slow part.** On 2026-10-31, across five runs (one per language), `evaluate` answered in 61–95 ms, `close_browser` in 20–69 ms and `tools/list` in 21–102 ms. `launch_browser` returned a running headless Chromium in 496–663 ms, while `navigate` took 3.3–7.3 s — the page loading through the proxy, not the protocol.
- **The snapshot is a third of the HTML, the product list a fifth.** The catalogue page's accessibility snapshot was 16,662 characters against 50,989 of HTML (33%); the snapshot of just its product list was 9,955 (20%), in every run. That is what the assistant reads to decide what to click ([pages to text for LLMs](https://cdpfleet.com/docs/cases/page-to-text-for-llms)).
- **The browser belongs to the API key, not to the MCP session.** With a new MCP session for every call, as ChatGPT makes them, the browser launched in the first session was still there in the next ones: the page opened in the second was the page the third read (20 books on page 2), and `close_browser` in the fourth ended that same browser session — in all five runs. A new MCP session cost 41–160 ms on top of the call.
- **Closing really closes.** After `close_browser`, a `navigate` was refused at once ("No browser is open. Call launch_browser first."), and `account` showed 0 live sessions in every run. A browser left open ends by its idle timeout instead, still billed until then.
- **Four official SDKs and plain HTTP all worked the same.** TypeScript, Python, Java and C# used their MCP SDKs; Go used about 260 lines of standard-library JSON-RPC (the whole program), since its official SDK needs Go 1.23. A script authenticates with `Authorization: Bearer <API key>`; assistants use OAuth instead ([MCP setup](https://cdpfleet.com/docs/mcp/setup)).
