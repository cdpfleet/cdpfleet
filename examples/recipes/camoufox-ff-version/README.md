# Camoufox reporting an older Firefox

`ff_version: 135` makes the user agent say Firefox 135 (`rv:135.0`) while the engine stays Camoufox's Firefox 152 — for sites that serve a different page by version. Feature checks still see Firefox 152's APIs, so don't expect old-browser behaviour.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
