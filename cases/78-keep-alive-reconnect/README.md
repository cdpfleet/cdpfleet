# keep_alive: an agent run that survives its client going away

**Medium** · 2026-10-30 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/keep-alive-reconnect)

> An agent works through a catalogue in a headless Chrome session with `keep_alive`, then its client disconnects. A new client reconnects to the same session and carries on. What survives, how fast the reconnect is, how the session looks while nobody is attached, when it ends, and what is billed.

## The problem

An agent that thinks between steps, a worker that gets redeployed, a laptop that loses Wi-Fi: each drops its connection, and without `keep_alive` a dropped connection ends the browser and everything in it. With `keep_alive` (dedicated threads) the session waits for a client to come back. This case runs a small agent workflow through a disconnect and a reconnect and measures what a second client finds, then lets the session run out to see when it ends and what the account is billed.

## What we used, and why

| What | Why |
|---|---|
| `"keep_alive": true` | A disconnect detaches the client instead of ending the session; `browser.close()` only disconnects. |
| `"inactivity_timeout": "30s"` | How long the session waits without a client (or any other traffic) before it ends. Short here so the run stays brief; up to 24 h on dedicated threads. |
| Two contexts, three pages | The "agent" context holds a catalogue page it paged through, a book page, a cookie and a localStorage entry; a second context holds one more page. |
| `browser.contexts()` after reconnecting | Over `wsUrl` the contexts a client created are listed again, with their pages. |
| `GET /v1/me/threads` | Whether the session is listed while nobody is attached, and when it stops being listed. |
| `GET /v1/me/sessions` and `/v1/me/usage` | The session's record (end reason, duration, weight) and the account's thread-seconds before, during the wait and after. |

## How it works

1. Launch headless Chrome with `keep_alive` and a 30-second `inactivity_timeout` on a dedicated key.
2. Connect, page from catalogue page 1 to page 2 in one context, set a cookie and a localStorage entry, open a book page in the same context and a category page in a second context.
3. Disconnect. 3 s later, look the session up in `/v1/me/threads`; reconnect 15 s after the disconnect.
4. Count the contexts and pages the new client sees, read the cookie and localStorage, then click on to page 3.
5. Disconnect for good. Poll `/v1/me/threads` until the session is gone, try to reconnect, and read the session's record and the account's usage.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Language | After the reconnect | Reconnect | While detached | After the client left | Billed | Measured |
|---|---|---|---|---|---|---|
| node | 2 contexts, 3 pages · cookie kept · localStorage kept · went on to page-3.html | 22 ms after 15.1 s away | listed in /v1/me/threads (running 13 s) | inactivity_timeout; unlisted after 61.1 s; reconnect then: 404 Not Found | 30 s × weight 1 (client connected or away for 29 s; live meter 49 s while it waited) | 2026-10-30 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The second client found everything the first one left.** On 2026-10-30, in all five runs (one per language), the client that reconnected 15 s after the disconnect saw both contexts and all three pages at the URLs they were on, the cookie and the localStorage entry, and clicked on from catalogue page 2 to page 3 as if nothing had happened. Over `wsUrl` the second context survived too; over `cdpUrl` only the default context does ([Edge keep-alive recipe](https://cdpfleet.com/docs/examples#edge-keep-alive)).
- **Reconnecting is fast.** `connect()` to the same `wsUrl` returned in 19–194 ms — the browser is already running, so there is no launch to wait for.
- **A detached session is still a running session.** 3 s after the disconnect it was listed in `/v1/me/threads`, its running time counting on (11–18 s), and it held its thread the whole time.
- **Without a client it ends by `inactivity_timeout`, and then it is gone for good.** After the last disconnect the session ended with `inactivity_timeout` (30 s here) and left `/v1/me/threads` 61–63 s after the client left: the timeout plus up to a minute before the router's next check of a detached session. A reconnect after that got `404 Not Found` on the `wsUrl`; nothing of the browser is left to resume. End a session you are done with at once with `DELETE /v1/me/threads/{sessionId}`.
- **What was billed: launch to the last disconnect.** Each session's record ran 29–39 s, within a second of the time from launch to the client's final disconnect, and the account's closed thread-seconds rose by exactly that. The 15 s away in the middle were billed; the final wait for the timeout was not, although `/v1/me/usage` counted it as live time (48–59 s) until the session ended. Measured with a 30 s timeout; set `inactivity_timeout` to how long your agent may really be away.
- **In Go, don't call `WaitForURL` on a reconnected page.** playwright-go 0.6000 panics there (it reads launch options that only the creating client has); the Go program polls `page.URL()` instead. Node, Python, Java and C# had no such issue.
