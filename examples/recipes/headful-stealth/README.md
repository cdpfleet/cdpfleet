# Patchright, headful, for stricter sites

A stealth-patched Chromium with a real display (2 threads): no `HeadlessChrome` anywhere, a normal window, and Playwright's automation flags removed. Patchright disables console events by design; see [Camoufox vs Patchright](https://cdpfleet.com/docs/compare/camoufox-vs-patchright).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
