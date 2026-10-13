# Can a page tell your automation client is listening? CDP and the console

**Medium** · 2026-10-13 · Chromium, Patchright, Google Chrome, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/cdp-detectability)

> The old Error.stack trick no longer fires, but console.log still costs more when Playwright listens over CDP. Patchright avoids it; Camoufox isn't CDP.

## The problem

Automation clients talk to Chromium over the DevTools protocol (CDP), and to deliver console messages they switch on its console and runtime domains (`Runtime.enable`). Anti-bot scripts have long tried to spot that from inside the page: a client that serializes logged objects reads things a normal browser never reads. Does that still work on today's browsers — and does it matter whether you connect with Playwright's protocol (`wsUrl`), over CDP (`cdpUrl`), with a console listener or without, with Patchright, or with Camoufox, which isn't CDP at all?

## What we used, and why

| What | Why |
|---|---|
| A self-contained test page | Loaded with `page.setContent`; its own script does the detecting, as a site's would, and writes the verdict into an attribute every client can read (Camoufox's `evaluate` runs in an isolated world). |
| The classic tell | Log an `Error` whose `stack` is a getter: a debugger that previews logged objects used to read it. |
| The same with `message` | An `Error` whose `message` is a getter. |
| The console cost | Log a 1,000-item array 40 times and time it against `JSON.stringify` of the same array (so the machine's speed cancels out); five rounds, the median ratio is kept. |
| Six setups | Playwright's Chromium and Patchright (the like-for-like pair: Patchright is that Chromium with Playwright's CDP traces removed), Chrome via `connect`, via `connectOverCDP` ([`cdp: true`](https://cdpfleet.com/docs/cdp)) with and without `page.on("console")`, and Camoufox. |

## How it works

1. Launch each setup headless, one after the other, and open a page in a new context (the default one over CDP).
2. Load the test page; wait for its verdict; count the console messages the client received.
3. Record whether each getter was read and the console-cost ratio.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-13):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | Error.stack getter read | Error.message getter read | console.log ÷ JSON.stringify | Console events received |
|---|---|---|---|---|
| Chromium, Playwright connect | no | yes | 3.98× | 1 |
| Patchright, Playwright connect | no | yes | 2.76× | 0 |
| Chrome, Playwright connect | no | yes | 3.98× | 1 |
| Chrome, connectOverCDP | no | yes | 4.23× | 1 |
| Chrome, connectOverCDP, no console listener | no | yes | 4.75× | (no listener) |
| Camoufox, Playwright connect | no | no | 0.14× | 1 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The classic tell is gone.** The `Error.stack` getter was read in none of the six setups, in every run and language — not with Playwright's protocol, not over CDP, not on Chrome 155 or Playwright's Chromium. A detection script still relying on it sees nothing.
- **Getter tricks now catch the browser, not the client.** The `Error.message` getter was read by every Chromium build — Patchright too, which delivers no console events at all — so it fires whether or not anyone is listening. It tells a site "this is Chromium", nothing about automation. Camoufox read neither getter.
- **The cost is still there.** Logging a big object costs more when Playwright's client receives the console: 3.5–4.0× a `JSON.stringify` on Playwright's Chromium and 3.1–4.8× on Chrome, against 2.6–2.8× on Patchright — the same Chromium base with Playwright's CDP traces removed. Across the five runs Patchright was always the cheapest Chromium setup.
- **Connecting over CDP or dropping the listener doesn't help.** Chrome via `connectOverCDP` cost the same as via `connect`, and without any `page.on("console")` it still cost 3.9–4.8×: Playwright switches the runtime domain on regardless, because `evaluate` needs it.
- **It's a timing signal, not a flag.** One launch's ratio moved by up to 1× between runs (each setup ran on another machine), so a site can only use it statistically. If a target scores this kind of signal, use [Patchright](https://cdpfleet.com/docs/compare/patchright-vs-rebrowser) (no console events, by design) or Camoufox (Firefox over Playwright's own protocol, 0.1–0.2× here — a different engine, so not comparable with the Chromium numbers). The other classic tells are in [case 16](https://cdpfleet.com/docs/cases/headless-tells); CDP access itself is on the [CDP page](https://cdpfleet.com/docs/cdp).
