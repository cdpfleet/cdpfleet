# Log in once, stay logged in: carrying a session between browsers

**Easy** · 2026-10-01 · Chromium, Firefox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/persist-login)

> Save cookies and localStorage at the end of one session, restore them in a fresh browser — even a different engine on a different server.

## The problem

Every cdpfleet session is a brand-new browser: when it closes, its cookies and storage are gone, and you never get the same browser back. Logging in for every session is slow, triggers security checks and burns accounts. You want to log in once and reuse that login in later sessions — maybe even in a different browser.

## What we used, and why

| What | Why |
|---|---|
| `context.storageState()` | Exports every cookie and each origin's localStorage as JSON — Playwright's portable "logged-in state". |
| A file on your machine | The state outlives the browser. It holds a live login, so treat it like a password. |
| `newContext({ storageState })` | Starts a context with that state already loaded, before the first request. |
| `chromium`, then `firefox` | Two different engines on purpose: the state format is engine-independent. |
| httpbin.org cookies + localStorage | Stand-ins for a real site's session cookie and saved draft. |

## How it works

1. Session 1 (Chromium): set two cookies and a localStorage value, save the state to a file, close the browser.
2. Session 2 (Firefox, a new browser on whichever server the fleet picks): open a context from the file and check what the site receives.
3. For comparison, a context without the state in the same browser.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-01):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Step | Browser | Cookies the site received | localStorage draft |
|---|---|---|---|
| session 1: logged in, state saved | chromium | session@httpbin.org, user@httpbin.org | (set) |
| session 2: restored from the file | firefox | {"session":"abc123","user":"alice"} | half-written review |
| session 2: new context, no state | firefox | {} | — |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The restored Firefox context sent both cookies and still had the localStorage draft** — a site would see the same logged-in user — while a context without the state sent nothing.
- **It works across engines and servers:** the state is plain JSON, a few hundred bytes here, not tied to the browser that made it.
- **Refresh it as you go:** sites rotate session cookies, so save the state again at the end of each session.
- **Keep the fingerprint consistent:** some sites tie a login to the device that made it. Restore the state into the same browser family, OS and country you logged in with if logins get challenged.
