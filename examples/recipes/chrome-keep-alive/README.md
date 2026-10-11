# Chrome you can disconnect from and come back to

`keep_alive: true` (dedicated threads) keeps the browser when your code disconnects: reconnect to the same `wsUrl` within `inactivity_timeout` and your contexts, pages and logins are still there. `browser.close()` only disconnects — end the session with `DELETE /v1/me/threads/{id}` or the SDK's `session.close()`.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
