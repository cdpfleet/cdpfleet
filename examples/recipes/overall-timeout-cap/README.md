# A hard time cap for batch jobs

`overall_timeout` ends the session at a fixed time after launch, busy or not. `inactivity_timeout` is different: it only ends a quiet session. Use the cap so a stuck loop, or a page that keeps the browser busy, can't bill past your budget. Measured with `"45s"`: a session loading a page every few seconds was disconnected 45.3 s after launch, mid-job. Values take `s`, `m` or `h`. The cap doesn't wait for your job, so save results as you go.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
