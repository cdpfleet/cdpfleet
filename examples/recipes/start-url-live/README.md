# Open a page on the server, take over in live view, then join from code

`start_url` makes the server open the page itself, so no client has to connect. To click and type in it, get a `control` live link (`POST https://<wsUrl host>/admin/session/live` with `{ "session_id": "…", "mode": "control" }`) or use **Try it live** in the dashboard. When your code connects, the page is already in `browser.contexts()[0]`: the code below waits for it instead of opening a new one. Measured on books.toscrape.com: the viewer went to the Travel category, and the code that joined afterwards found that page with its 11 books. Headless needs `live_view: true` on a Chromium-based browser; Firefox, Camoufox and WebKit are headful only. See [Launch interactive](https://cdpfleet.com/docs/live-view#launch-interactive).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
