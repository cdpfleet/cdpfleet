# New tabs, window.open and the three blocking dialogs: everything that interrupts a script

**Easy** · 2026-10-06 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/popups-and-dialogs)

> A page that opens tabs and throws alert, confirm and prompt at you, handled on a remote browser: catching the new page, answering dialogs, and what happens when you don't.

## The problem

Real sites open things: a link with `target="_blank"`, a login that pops a window, a "Delete 3 items?" confirm, a prompt. Each one changes what your script's `page` object refers to or blocks it until someone answers. On a local browser you'd see it happen; on a remote browser you see a timeout. The handling isn't hard, but it has to be in place *before* the click, and the default behaviour when nothing is in place isn't what most people assume.

## What we used, and why

| What | Why |
|---|---|
| `page.setContent(HTML)` | The test page is served by the browser itself: a new-tab link, a `window.open` button, and buttons that call `alert`, `confirm` and `prompt`. No third-party site to flake. |
| `context.waitForEvent("page")` started before the click | The new page can appear before the click resolves; waiting afterwards can miss it. |
| `popup.waitForLoadState()`, `popup.opener()` | A popup starts empty; wait for it to load before reading `url()` or `title()`. `opener()` ties it back to the page that opened it. |
| `page.on("dialog", …)` with `accept()`, `accept(text)` | You answer the dialog; the page gets `true`, your text, or `false`/`null` if you dismiss. |
| Clicking `confirm` once with no handler | To show the default: Playwright dismisses dialogs it has no handler for, so `confirm()` returns `false`. |

## How it works

1. Load the test page into a fresh context.
2. Click the `target="_blank"` link and the `window.open` button; each time, capture the new page via the context event, wait for it to load, read URL and title, check `opener()`, close it.
3. Click `confirm` with no dialog handler and read what the page got back.
4. Install a dialog handler that accepts everything (and types a name into the prompt); click `alert`, `confirm`, `prompt`; read what the page got back.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-06):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Event | What happened | Handled with | Pages open | opener() is the main page |
|---|---|---|---|---|
| link with target=_blank | new page: https://example.com/ — "Example Domain" | context.waitForEvent("page") + popup.waitForLoadState() | 2 | true |
| window.open() | new page: https://example.com/?popup — "Example Domain" | context.waitForEvent("page") + popup.waitForLoadState() | 2 | true |
| confirm() with no dialog handler | page saw confirm() return false | nothing — auto-dismissed | 1 | — |
| alert() | alert: "Saved!" | dialog.accept() | 1 | — |
| confirm() with a handler | confirm: "Delete 3 items?" → page saw true | dialog.accept() | 1 | — |
| prompt() | prompt: "Your name?" → page saw "Ada Lovelace" | dialog.accept("Ada Lovelace") | 1 | — |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Both kinds of new page arrive the same way** — `context.waitForEvent("page")` catches a `target="_blank"` link and a `window.open` call alike, `opener()` points back at the main page, and after `waitForLoadState` the popup is a normal `page` you can drive. Both counted as a second page in the context, not a second session: tabs are free, threads are per session.
- **Unhandled dialogs are dismissed, not accepted:** with no handler, `confirm("Delete 3 items?")` returned `false` to the page — the script continued, and the site silently took the "cancel" path. That is the opposite of the usual assumption.
- **With a handler you choose the answer**: `alert` acknowledged, `confirm` → `true`, `prompt` → `"Ada Lovelace"`. The handler must exist before the click, and it must call `accept()` or `dismiss()`, or the page stays blocked.
- **Nothing here depends on the browser being remote** — the events travel over the same connection as everything else; the only remote-specific cost is that a missed popup looks like a timeout rather than a visible window.
