# Brave with Shields on

Brave sends `Brave` in the client-hint brands (its user agent says Chrome), adds a `Sec-GPC` header and randomises cores, screen and canvas between sessions. See [Chrome vs Brave](https://cdpfleet.com/docs/compare/chrome-vs-brave). Headless here (1 thread).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
