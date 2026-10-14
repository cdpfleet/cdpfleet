# Opera over CDP (Puppeteer or connectOverCDP)

`"cdp": true` on Opera returns a `cdpUrl` for Puppeteer or Playwright's `connectOverCDP` in any version. Headless over CDP, open pages in a new context (`browser.newPage()` does) — Opera doesn't render pages in its default context there, so `keep_alive` over CDP needs headful Opera. Pages see the `Opera` client-hint brand and `OPR/` in the user agent ([Opera vs Chrome](https://cdpfleet.com/docs/compare/opera-vs-chrome)). See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
