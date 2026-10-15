# Camoufox without WebGL

`block_webgl: true` turns WebGL off — no GPU fingerprint at all, and less work per page. A missing WebGL is itself a signal some sites score, so prefer a plausible GPU with `webgl_config` when they do (see [stealth builds](https://cdpfleet.com/docs/cases/stealth-builds)).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
