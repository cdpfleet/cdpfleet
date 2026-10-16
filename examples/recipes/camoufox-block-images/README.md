# Camoufox without images, for less proxy traffic

`block_images: true` stops Camoufox from requesting images at all. Measured on books.toscrape.com's front page: 10 requests and ~152 KB instead of 30 requests and ~331 KB, loaded in ~1.6 s instead of ~7 s, with all 20 product titles still there. Read image URLs from `src` attributes; screenshots show no pictures. On Chromium-based browsers block images with `page.route` instead ([bandwidth diet](https://cdpfleet.com/docs/cases/bandwidth-diet)).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
