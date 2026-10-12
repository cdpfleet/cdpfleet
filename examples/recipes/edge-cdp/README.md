# Microsoft Edge over CDP (Puppeteer or connectOverCDP)

`"cdp": true` works on Edge as on Chrome: connect to `cdpUrl` with Playwright's `connectOverCDP` in any client version, or with Puppeteer. Pages see Edge's own brand in the client hints (see [Chrome vs Edge](https://cdpfleet.com/docs/compare/chrome-vs-edge)). See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
