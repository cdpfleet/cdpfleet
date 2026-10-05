# Short-lived sessions for burst jobs

For many small jobs, make each session end itself: 15 seconds idle or 2 minutes total, whichever comes first, so a hung script never bills for long. Values take `s`, `m`, `h`.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
