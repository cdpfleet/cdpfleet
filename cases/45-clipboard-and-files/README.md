# Clipboard and file permissions on remote browsers: what's blocked and what works

**Medium** · 2026-10-14 · Chromium, Google Chrome, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/clipboard-and-files)

> The clipboard and file APIs on Chromium, Chrome, Firefox and WebKit: which calls are denied by default, which permission names each engine accepts, why `setFiles` can't answer `showOpenFilePicker`, and the workarounds.

## The problem

Web apps copy and paste through `navigator.clipboard`, take files through `<input type="file">` or the newer File System Access picker (`showOpenFilePicker`), and keep data in the origin-private file system. On a remote browser nobody can click "Allow" on a permission prompt or pick a file in a native dialog — so which of these work out of the box, which need a permission granted, and which need a different approach entirely?

## What we used, and why

| What | Why |
|---|---|
| `context.route()` on a made-up `https://` origin | A secure context — the clipboard and file APIs require one — served by Playwright, with no network traffic. |
| `navigator.clipboard.writeText` / `readText` | The async clipboard API, each capped at 3 s so an unanswered prompt shows up as a timeout instead of a hang. |
| `context.grantPermissions(["clipboard-read", "clipboard-write"])` | Pre-answers the clipboard prompts — with Chromium's permission names. |
| `page.waitForEvent("filechooser")` + `setFiles()` | Playwright's way to answer a file dialog without a native window. |
| `navigator.storage.getDirectory()` | The origin-private file system: a sandboxed folder per site that needs no picker and no permission. |
| `context.addInitScript()` | Replaces `showOpenFilePicker` before the page loads, so the page's own code receives a file we provide. |

## How it works

1. For each of four browsers, open the test page in a fresh context and round-trip a string through the clipboard with no permissions.
2. Click the file input, answer the file chooser with `setFiles()`, and read back what the page received.
3. Click the button that calls `showOpenFilePicker()` and wait for a `filechooser` event; write and read a file in the origin-private file system.
4. In a new context, stub `showOpenFilePicker` with an init script and click the button again.
5. In another context, grant `clipboard-read` and `clipboard-write` and repeat the clipboard round trip.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-14):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Clipboard write / read (default) | grantPermissions | Write / read (granted) | <input type=file> | Picker → filechooser | Picker stubbed | Private file system |
|---|---|---|---|---|---|---|---|
| Chromium, headless | NotAllowedError / NotAllowedError | accepted | ok / ok | report.csv:14 | no event | report.csv:14 | ok |
| Chrome, headful | ok / timed out (prompt?) | accepted | ok / ok | report.csv:14 | no event | report.csv:14 | ok |
| Firefox, headful | ok / ok | Unknown permission: clipboard-read | — | report.csv:14 | no picker API | report.csv:14 | ok |
| WebKit, headful | ok / NotAllowedError | Unknown permission: clipboard-write | — | report.csv:14 | no picker API | report.csv:14 | no (TypeError) |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Clipboard defaults differ by engine:** headless Chromium denies both writing and reading (`NotAllowedError`); headful Chrome lets a page write but reading waits on a prompt nobody answers; Firefox allowed both; WebKit allowed writing and denied reading.
- **Grant permissions with Chromium's names on Chromium only:** `grantPermissions(["clipboard-read", "clipboard-write"])` made both work on Chromium and Chrome. Firefox and WebKit reject those names (`Unknown permission`), and Firefox only says so when the next page opens — guard the call per engine.
- **`<input type="file">` works everywhere:** the `filechooser` event fired on all four and `setFiles()` delivered the file (name and size as the page saw them).
- **`setFiles()` can't answer `showOpenFilePicker()`:** Chromium and Chrome have the File System Access picker, but clicking it raised no `filechooser` event — Playwright has nothing to hand the file to. Stub the function with `addInitScript` instead: the page's own code then got our file on every engine.
- **The origin-private file system** wrote and read back on Chromium, Chrome and Firefox with no permission; WebKit has no `navigator.storage` there.
