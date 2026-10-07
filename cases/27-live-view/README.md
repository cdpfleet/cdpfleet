# Watch it live: streaming a remote browser to your code, frame by frame

**Easy** · 2026-10-07 · Chromium, Firefox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/live-view)

> Playwright 1.60's page.screencast on Chromium and Firefox: how many frames arrive while a page loads, scrolls and sits still — and why the two engines behave nothing alike when nothing moves.

## The problem

A remote browser is a black box while your code runs. Hosted services sell a "live view" URL for that; Playwright 1.60 can do it itself: `page.screencast.start({ onFrame })` sends a JPEG of the page to your code whenever the page changes, over the session's own connection. That's enough to build a viewer, a debugging dashboard or a thumbnail strip — if you know how much it sends. How many frames per second, how big, and what happens when the page is idle?

## What we used, and why

| What | Why |
|---|---|
| `page.screencast.start({ quality: 60, onFrame })` | Starts the stream; each callback gets `data` (JPEG bytes) and the viewport size. Needs a Playwright 1.60+ client. [Live view](https://cdpfleet.com/docs/live-view) has a relay to a browser tab. |
| `chromium` and `firefox`, headless | The two engines families encode frames differently; WebKit behaves like Chromium here. |
| Three phases | Load a long Wikipedia article, scroll it ten times in four seconds, then touch nothing for three seconds. Frames are counted per phase. |
| The JPEG magic bytes `FF D8` | Every frame is checked to be a real image. |
| Retries on navigation and on `503` | The proxy can drop a tunnel and a browser can be momentarily out of capacity; both are retried. |

## How it works

1. Launch the browser, open a page and start the screencast with a callback that tags every frame with the current phase.
2. Load the article, scroll it, then wait three seconds without doing anything.
3. Stop the screencast and count frames per phase, the frame rate while scrolling, the average size and the viewport.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-07):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Frames while loading | Frames while scrolling | fps while scrolling | Frames in 3 s idle | Avg KB / frame | All JPEG | Viewport |
|---|---|---|---|---|---|---|---|
| chromium | 52 | 32 | 7.6 | 1 | 40 | true | 1280x720 |
| firefox | 320 | 95 | 21.6 | 68 | 42 | true | 1280x720 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **It just works over the session connection:** real JPEG frames at the page's 1280×720 viewport, from both engines, with no launch option and no extra service.
- **Chromium sends frames only when pixels change:** a couple of frames a second while scrolling through a proxy, and none at all on an idle page — cheap enough to leave on.
- **Firefox streams continuously:** many more frames while scrolling — and it kept sending them on an idle page (dozens in three seconds; we measured the same on a completely static page). Camoufox uses the same engine. Throttle in your callback if you only need a preview.
- **Budget the bytes:** about 40–50 KB per frame at quality 60 and full viewport. Lower `quality`, or in Node.js pass `size` to scale frames down, when you relay them.
- **View-only, your connection only:** frames go to the client that drives the page; to show them elsewhere, forward them (a WebSocket relay is ten lines).
