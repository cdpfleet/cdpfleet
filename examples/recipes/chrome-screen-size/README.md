# Headful Chrome on a full-HD screen

`screen_size` sets the virtual display of a headful session (1920×1080 here). Pages only see it with a context created with `viewport: null` (Python `no_viewport=True`) — otherwise Playwright emulates a screen the size of the viewport, 1280×720 by default. Also fills recordings and live view edge to edge.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
