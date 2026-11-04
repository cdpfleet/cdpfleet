# Keep the files a session downloads, fetch them afterwards

`"downloads": true` keeps every file the browser downloads in the session, so your code doesn't have to call `saveAs()` or stay connected while files copy. After the session, `GET /v1/me/downloads?session_id=…` lists each file with `name`, `bytes`, `sha256` and `source_url`, and `GET /v1/me/downloads/{id}/url` returns a private link for 15 minutes. Measured on 2026-11-04: two files (200 KB and 30 bytes) started with a click and never saved by the client were listed 1.1 s after `browser.close()`, and the fetched bytes matched the listed sha256 and the original file. Paid plans; files are kept 2 days, up to 1 GB and 200 files. See [Downloads](https://cdpfleet.com/docs/downloads).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
