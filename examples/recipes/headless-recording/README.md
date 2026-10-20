# Record a headless session and get the video

`record` is on by default, but a headless session is only recorded with `"live_view": true` or `"cdp": true`, as here. Measured with this launch and about 20 s of browsing: 854×480 (197 KB) on the free tier, and 1280×720 (366 KB) on a 720p recording plan. On a 1080p plan the video follows the page's viewport. With the default 1280×720 page it stayed 1280×720, while a `browser.newContext({ viewport: { width: 1920, height: 1080 } })` page recorded 1920×1080 (479 KB). The video is listed seconds after the session ends: `GET /v1/me/recordings`, then `GET /v1/me/recordings/{id}/url` for a 15-minute link. See [Session recording](https://cdpfleet.com/docs/recordings).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
