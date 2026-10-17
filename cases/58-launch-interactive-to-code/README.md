# Do it by hand in live view, then let your code finish in the same browser

**Medium** · 2026-10-17 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/launch-interactive-to-code)

> Launch a browser with `start_url` and no client, drive it through a control-mode live link — address bar, Tab, typing — then connect your code: it finds the person's page in `contexts()[0]`, reads what they typed and submits the form. The watched session outlived its 30 s idle timeout; an unwatched twin ended at 30 s.

## The problem

Some steps are easier by hand: finding out why a page blocks your scraper, clicking through a consent wall, typing a one-time code, or just trying a browser and proxy before writing any code. With [Launch interactive](https://cdpfleet.com/docs/live-view#launch-interactive) a person does those steps in a live view, and the script picks up exactly where they stopped — same browser, same cookies, same page — instead of replaying everything from scratch. This case does that end to end over the API, which is also how the dashboard's "Try it live" works.

## What we used, and why

| What | Why |
|---|---|
| `start_url` (launch option) | The node opens this page itself, so the session runs before any client connects. Headless Chromium needs `live_view: true` to be watchable. |
| `POST https://<wsUrl host>/admin/session/live` with `mode: "control"` | A single-use live link. For headless sessions it is a screencast WebSocket: JPEG frames in, and `navigate`, `key` and `text` messages out. |
| `inactivity_timeout: "30s"` | Short on purpose, to see what counts as activity: input from the person, and an open viewer. |
| `browser.contexts()[0].pages()` | Where your code finds the page the person was on when it connects to `wsUrl`. |
| `GET /v1/me/sessions` | The account API's history: how long each session ran and why it ended. |
| `proxy` | Your own proxy; a few KB of traffic. |

## How it works

1. Launch two identical sessions with `start_url: https://httpbin.org/` and a 30 s `inactivity_timeout`; open a control-mode live link for the first one only.
2. In the live view, go to the order form from the address bar (`navigate`) and fill the first three fields from the keyboard: Tab, then type. In the programs this person is a scripted stand-in sending the same messages the dashboard's viewer sends.
3. Stop all input and just watch for 45 s.
4. Try to connect to the unwatched twin, then connect to the watched session and list the pages in its default context.
5. Read the three typed fields, pick a size and a topping, set a delivery time and submit — httpbin echoes the form back. Close, and read both sessions from the history.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-17):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| What | Result |
|---|---|
| Live link protocol | screencast |
| Pages seen by the viewer | / → /forms/post → /post |
| Typed in the live view (read by code) | Ada Lovelace · +44 20 7946 0000 · ada@example.com |
| Submitted by code | size medium, topping cheese, delivery 19:30 |
| Watched session after 45 s without input | alive |
| Unwatched twin after 45 s | ended |
| Watched session ran / ended because | 68 s / client_disconnected |
| Unwatched twin ran | 30 s |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Your code lands on the person's page:** `contexts()[0].pages()` held exactly one page, the form, with the name, phone and e-mail typed in the live view. The code read them and submitted the order; httpbin received all seven fields.
- **The viewer follows the code too:** the live view's `pages` messages went from the start page to the form (the person) to `/post` (the code), so whoever is watching sees the script take over.
- **An open viewer counts as activity:** 45 s without input didn't end the watched session, though its `inactivity_timeout` was 30 s. The unwatched twin, launched with the same options, ended at 30 s and refused the connection.
- **Disconnecting ends it:** without `keep_alive`, the session ended when the code closed its connection (`client_disconnected`) — 50–80 s after launch in these runs, well past the 30 s idle timeout. Billing is that of a normal session.
- Use it when a step needs a person or you're still exploring; keep the parts a script does reliably — choices, submits, extraction — in code. In the dashboard the same flow is Code generator → **Try it live** → **Launch interactive**, with the generated code for the identical browser next to it.
