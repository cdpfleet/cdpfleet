# Chrome over CDP (connectOverCDP or Puppeteer)

Launch with `"cdp": true` and connect to `cdpUrl` with Playwright's `connectOverCDP` — no client version matching — or with Puppeteer (`puppeteer.connect({ browserWSEndpoint: cdpUrl, headers })`). See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
