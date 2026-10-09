# Opera, developer channel

Run the next Opera before it ships: the channel is called `"developer"` (not `"dev"`). Today that is Opera 138 on Chromium 153 — the user agent ends in `OPR/138.0.0.0 (Edition developer)` and the client hints say `Opera`. Headless here (1 thread), so the user agent says `HeadlessChrome`; use `headless: false` where that matters.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
