# Brave over CDP (Puppeteer or connectOverCDP)

`"cdp": true` on Brave returns a `cdpUrl` for Puppeteer or Playwright's `connectOverCDP` in any version. Brave keeps its own identity over CDP too: the `Brave` client-hint brand, `Sec-GPC` and randomised fingerprint values ([Chrome vs Brave](https://cdpfleet.com/docs/compare/chrome-vs-brave)). See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
