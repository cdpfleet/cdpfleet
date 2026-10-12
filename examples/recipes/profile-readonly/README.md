# Many scrapers sharing one saved login

`"mode": "readonly"` restores a profile without saving it back, so any number of sessions can share one login in parallel while a single normal session keeps it fresh (a second writer gets `409 profile_in_use`). See [Persistent profiles](https://cdpfleet.com/docs/profiles).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
