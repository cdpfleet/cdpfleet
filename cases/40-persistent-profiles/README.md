# Persistent profiles: what a saved browser profile keeps, and what it doesn't

**Easy** · 2026-10-12 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/persistent-profiles)

> Save a cdpfleet browser profile and restore it: which cookies and storage come back, the newContext() trap, read-only sharing, one writer at a time.

## The problem

Logging in on every run is slow, and on sites with a second factor it can't be automated without the secret. A [persistent profile](https://cdpfleet.com/docs/profiles) keeps the browser's state on cdpfleet between sessions: launch with `"profile": "<name>"`, and the next launch with the same name starts where the last one ended. But "where it ended" means what a browser keeps when it restarts — and that isn't everything a page had. We write every kind of browser storage in one session and look at what the next session gets back.

## What we used, and why

| What | Why |
|---|---|
| `"profile": "case40-…"` | The launch option. The first launch with a new name creates the profile; it's saved when the session ends, after the browser has exited. Each run uses a fresh name and deletes the profile at the end (`DELETE /v1/me/profiles/{name}`). |
| `browser.contexts()[0]` | The profile is the browser's existing context. The programs open their pages there, not in `browser.newContext()`. |
| [authenticationtest.com](https://authenticationtest.com/totpChallenge/) | A public practice login with a TOTP second factor (the code is generated as in the [TOTP login case](https://cdpfleet.com/docs/cases/totp-login)); the account, password and secret are the demo values the site publishes. |
| Five kinds of storage on httpbin.org | A cookie with an expiry (what "Remember me" sets), a cookie without one (a session cookie), localStorage, sessionStorage and an IndexedDB record. |
| `{ "id": …, "mode": "readonly" }` | Read-only launches of the same profile while another session holds it. |

## How it works

1. Session A launches with a new profile name: log into the practice site (with the TOTP code) and write the five kinds of storage. Close the browser, which ends the session, and wait until `GET /v1/me/profiles` shows version 1.
2. Session B launches with the same name: read the storage back in `contexts()[0]`, open the practice site, and read the storage in a `newContext()` too.
3. While B is open, launch the profile again normally, then three times read-only.
4. Close B, wait for version 2, delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-12):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Storage | Session A wrote | Session B (profile) | Session B, newContext() |
|---|---|---|---|
| Cookie with an expiry ("Remember me") | yes | kept | empty |
| Session cookie (no expiry) | yes | gone | empty |
| localStorage | yes | kept | empty |
| sessionStorage | yes | gone | empty |
| IndexedDB | yes | kept | empty |
| Practice-site login (session cookie) | signed in | signed out |  |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **What came back:** the cookie with an expiry, localStorage and the IndexedDB record — in every run and every language. That is where "Remember me" logins, site preferences and IndexedDB-based logins such as Firebase Auth's live.
- **What didn't:** the session cookie and sessionStorage. A restored profile is a browser restart, and browsers drop both on restart. The practice site keeps its login in a session cookie (no expiry), so session B was signed out: **profiles don't keep logins that live in session cookies.** For those, save the state with Playwright's `storageState()`, which does include session cookies ([TOTP login](https://cdpfleet.com/docs/cases/totp-login), [persisting logins](https://cdpfleet.com/docs/cases/persist-login)), or use the site's "Remember me" option where it has one, which usually sets a cookie with an expiry.
- **Work in `browser.contexts()[0]`.** A `newContext()` in the same session started empty — no cookies, no localStorage, no IndexedDB — and, per [the docs](https://cdpfleet.com/docs/profiles), nothing in it is saved. This is the mistake to avoid when moving existing code to profiles. (Go: playwright-go 0.6000's `WaitForURL` panics on pages in `contexts()[0]`, a context the client didn't create, so the Go program polls `page.URL()` instead.)
- **One writer, many readers.** While session B held the profile, a second normal launch got `409 profile_in_use`; three read-only launches all got the saved state, and the profile stayed at version 1 until B ended and saved version 2.
- **Small and quick:** the saved profile was 4.7–4.8 KB across the five runs ([caches are left out](https://cdpfleet.com/docs/profiles)), and a launch with a profile — new or restored — took 0.8–1.7 s from the launch call to a connected page.
