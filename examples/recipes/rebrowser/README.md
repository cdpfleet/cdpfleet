# Rebrowser with the Playwright 1.52 client

Rebrowser is a patched Chromium that needs the 1.52 Playwright client — the generated code uses it. Headful by default (2 threads); see [stealth builds](https://cdpfleet.com/docs/cases/stealth-builds) for how it compares.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
