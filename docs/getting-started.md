# Getting started

1. **Get an API key** in the [dashboard](https://cdpfleet.com/app) → API keys. It's shown once; keep it in an environment variable, e.g. `CDPFLEET_API_KEY`.
2. **Have a proxy.** Every session browses through a proxy you provide (HTTP, HTTPS or SOCKS5). You can save and test proxies in the dashboard → Proxies.
3. **Install Playwright 1.60** for your language — the client must match the server's minor version:

   | Language | Install |
   |---|---|
   | Node.js | `npm install playwright@1.60.0` |
   | Python | `pip install playwright==1.60.0 requests` |
   | Java | `com.microsoft.playwright:playwright:1.60.0` |
   | C# | `dotnet add package Microsoft.Playwright --version 1.60.0` |
   | Go | `go get github.com/playwright-community/playwright-go@v0.6000.0` ([driver setup](../templates/go/README.md)) |

   No `playwright install` is needed — the browsers run on cdpfleet. (Rebrowser needs Playwright **1.52**.)
4. **Run a quickstart:** [examples/quickstart](../examples/quickstart), or copy a [starter project](../templates).

## How a session works

1. `POST https://starter.cdpfleet.com/<browser>/session` with your key and launch options → `{ sessionId, wsUrl, … }`.
2. Connect to `wsUrl` with Playwright's `connect()` for the browser's family (`chromium`, `firefox` or `webkit`), sending your key again.
3. Drive it: pages, navigation, screenshots, request interception — anything Playwright does.
4. `browser.close()` ends the session immediately and stops billing.

Pick a browser and options and get ready-to-run code in the [code builder](https://cdpfleet.com/docs/builder).
