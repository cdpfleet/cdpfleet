# CloakBrowser over CDP (Puppeteer or connectOverCDP)

CloakBrowser takes `"cdp": true` too, so Puppeteer-based tooling can use its Chrome persona ([CloakBrowser vs Patchright](https://cdpfleet.com/docs/compare/cloakbrowser-vs-patchright)). A CDP client can add tells of its own that a Playwright `connect()` doesn't — see [CDP detectability](https://cdpfleet.com/docs/cases/cdp-detectability) before choosing CDP for a stealth build. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
