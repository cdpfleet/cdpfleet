# Files across the wire: uploading to and downloading from a remote browser

**Medium** · 2026-10-01 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/files-up-and-down)

> The browser runs on our servers, your files live on your machine. setInputFiles and download.saveAs move them both ways — byte for byte.

## The problem

Automations often upload a document (a CSV import, a profile photo) or download one (an invoice, an export). With a remote browser, the upload has to come from your disk and the download has to end up on your disk — not on the server running the browser. Does that work transparently, and are the files intact?

## What we used, and why

| What | Why |
|---|---|
| `page.setInputFiles(selector, path)` | Reads the file on your machine and streams it to the remote browser's file input. |
| `page.waitForEvent('download')` | Catches the download the click starts; the file is first saved on the browser's server. |
| `download.saveAs(path)` | Streams the downloaded file from the server to your machine. |
| `page.route` on httpbin.org | Serves a tiny page with an upload form and a download link on a real origin; httpbin echoes uploads back. |
| `/bytes/102400?seed=42` | httpbin's seeded random bytes: the same seed always gives the same 100 KB, so the download can be checked against a direct copy. |

## How it works

1. Write a ~60 KB CSV locally and hash it.
2. Set it into the page's file input and submit; compare the hash of what the server received.
3. Click the download link, save the file locally and compare its hash with the same bytes fetched directly.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-01):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Direction | Bytes | Identical | Time (ms) |
|---|---|---|---|
| upload cdpfleet-upload.csv | 58902 | true | 3134 |
| download data.bin | 102400 | true | 1286 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Both directions are byte-exact:** the uploaded CSV and the downloaded file hash identically on both ends.
- **No extra work for "remote":** the same Playwright calls you'd use locally; the client streams the files over the session's WebSocket.
- **Files never reach your machine on their own:** a download stays on the server until you call `saveAs` (or read `createReadStream`) — and is deleted with the session.
- Big files cost transfer time over the WebSocket and proxy bandwidth for what the page itself downloads; check both before moving gigabytes.
