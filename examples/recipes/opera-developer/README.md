# Opera, developer channel

Run the next Opera before it ships: the channel is called `"developer"` (not `"dev"`). On 2026-11-08 that was Opera 140 on Chromium 155 — the user agent ends in `OPR/140.0.0.0 (Edition developer)` and the client hints say `Opera` (the [developer vs stable comparison](https://cdpfleet.com/docs/compare/opera-developer-vs-stable) has the rest). Headless here (1 thread), so the user agent says `HeadlessChrome`; use `headless: false` where that matters.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
