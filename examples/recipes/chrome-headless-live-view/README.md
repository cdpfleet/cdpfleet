# Headless Chrome a person can watch and take over

Headful sessions are always watchable; a headless one needs `live_view: true` at launch. Then `POST /admin/session/live` on the session's router returns a screencast `live_url` (view or control) — even before your code connects — and the session is recorded. See [Live view](https://cdpfleet.com/docs/live-view) and [Session recordings](https://cdpfleet.com/docs/recordings).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
