# Session replay without a vendor: Playwright tracing and video on a remote browser

**Easy** · 2026-10-04 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/trace-and-video)

> Record a time-travel trace (actions, DOM snapshots, screenshots, network) and a video of a session running on the fleet, pull both to your machine, and open them with the tools you already have.

## The problem

When a remote browser does the wrong thing at step 7 of 12, a stack trace tells you nothing. Hosted-browser vendors sell "session replay" dashboards for this. Playwright has had both halves built in for years — tracing (every action with a before/after DOM snapshot, screenshots and the network log, viewable in a time-travel UI) and video recording — and both are designed to work on browsers you only reach over a WebSocket. The question is whether they work through a fleet connection, what you get, and how big it is.

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | Video and tracing work headless; the recording is of the renderer, not of a screen. |
| `browser.newContext({ recordVideo: { dir, size } })` | Video is captured on the remote browser per page; the file is fetched when the page or context closes and saved where you say with `video.saveAs()`. |
| `context.tracing.start({ screenshots: true, snapshots: true })` / `tracing.stop({ path })` | The trace is assembled by your client from events streamed over the connection and written as a zip on your side. |
| `page.video()` captured before closing | The `Video` handle must be taken while the page is open; `saveAs` is called after the context has closed and the file is complete. |
| A small zip reader | To look inside `trace.zip` without extra dependencies: the trace is newline-delimited JSON plus JPEG screenshots. |

## How it works

1. Launch headless Chromium, open a 1280×720 context with video recording, start tracing.
2. Run a short flow: load example.com, open an HTML form on httpbin.org, fill a field, check a box, submit, wait for the result page, take a screenshot.
3. Stop tracing to `trace.zip`, close the context (which finishes the video), save the video as `session.webm`, close the browser.
4. Open the zip: count the recorded actions, DOM snapshots, screenshots and network entries; check the video is a WebM file; print sizes and how to open each artifact.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-04):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Artifact | Bytes | What's in it | Screenshots | DOM snapshots | Network entries | Open with |
|---|---|---|---|---|---|---|
| trace.zip | 343204 | 8 actions: newPage, goto, goto, fill, check, click, waitForEventInfo, screenshot | 8 | 16 | 4 | npx playwright show-trace trace.zip |
| session.webm | 217664 | valid WebM (EBML header) | — | — | — | any video player |
| final.png | 77996 | screenshot after the last step | — | — | — | image viewer |
| whole run | — | 10.3 s including recording and downloads | — | — | — | — |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Both recordings work over the fleet connection with no server-side feature:** the trace zip and the WebM video land on your machine, and the whole run — recording, downloads included — took a few seconds.
- **The trace holds the whole story:** eight actions (`newPage`, two `goto`, `fill`, `check`, `click`, the URL wait, `screenshot` — seven in Go, whose `WaitForURL` is a client-side wait the trace doesn't record), a DOM snapshot before and after each, ten screenshots, the network log. `npx playwright show-trace trace.zip` opens the time-travel viewer; Java, C# and Go users can use the Node CLI or [trace.playwright.dev](https://trace.playwright.dev).
- **It's small:** about 360 KB for the trace and 230 KB for a 1280×720 video of this flow — cheap enough to record every run and keep the ones that failed.
- **Order matters:** take `page.video()` while the page is open, stop tracing before closing the context, close the context before `saveAs`. Get it wrong and you get an empty file or a hang, not an error.
- **What you don't get:** a live view while the session runs. For that, poll screenshots, or see the [Account API](https://cdpfleet.com/docs/api) for live session state.
