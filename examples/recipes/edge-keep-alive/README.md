# Microsoft Edge over CDP that survives a disconnect

`keep_alive: true` (dedicated threads) with `"cdp": true`: disconnect from `cdpUrl`, reconnect within `inactivity_timeout` and your pages are where you left them. Over CDP only the default context survives, so the code below opens its page in `browser.contexts()[0]` (a context from `browser.newPage()` or `newContext()` is gone after a reconnect). Measured on headless Edge: the default context kept its page and localStorage across a reconnect; a second context was gone. `browser.close()` only disconnects — end the session with `DELETE /v1/me/threads/{id}`. See [CDP & Puppeteer](https://cdpfleet.com/docs/cdp).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
