# Yandex Browser over CDP (Puppeteer or connectOverCDP)

`"cdp": true` works on Yandex Browser as on Chrome: connect Puppeteer or Playwright's `connectOverCDP` (any client version) to `cdpUrl`. Handy when existing Puppeteer code has to run on Yandex Browser. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
