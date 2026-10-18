# Headful Opera over CDP that survives a disconnect

`keep_alive: true` (dedicated threads) with `"cdp": true` on headful Opera: disconnect, reconnect to the same `cdpUrl` within `inactivity_timeout`, and the default context, where the code below opens its page, is where you left it. Measured: a books.toscrape.com category page and its localStorage were still there after a reconnect 8 s later. It has to be headful: over CDP only the default context survives a reconnect, and in headless Opera that context has no window (a screenshot fails with "0 width"). `browser.close()` only disconnects; end the session with `DELETE /v1/me/threads/{id}`. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
