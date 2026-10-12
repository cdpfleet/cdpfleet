# Saved the login, still logged out: storageState and IndexedDB

**Medium** · 2026-10-12 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/indexeddb-login-state)

> Firebase Auth, and apps built on SDKs that persist to IndexedDB, keep the login there, which storageState leaves out by default. One option brings it along; Map and Blob values still don't survive.

## The problem

You log in once, save the state with `storageState()`, restore it in the next session — and the app greets you with a sign-in form. The cookies and localStorage came back, but the login wasn't in them: Firebase Auth, and apps built on SDKs that persist to IndexedDB, keep the signed-in user and its refresh token in **IndexedDB** (most other auth SDKs default to localStorage, sessionStorage or memory). By default Playwright doesn't save IndexedDB at all. [Case 11](https://cdpfleet.com/docs/cases/persist-login) carried cookies and localStorage between browsers; this one carries the rest.

## What we used, and why

| What | Why |
|---|---|
| A Firebase-shaped login | The record Firebase Auth's web SDK writes after sign-in: database `firebaseLocalStorageDb`, store `firebaseLocalStorage`, key `firebase:authUser:<apiKey>:[DEFAULT]`, the user and their tokens as the value. Plus a cookie and a localStorage value, the way most sites mix them. |
| `storageState()` | The default export: cookies and each origin's localStorage. |
| `storageState({ indexedDB: true })` | Also exports every IndexedDB database of every origin the context visited (Playwright 1.51+). Python `indexed_db=True`, Java `setIndexedDB(true)`, C# `IndexedDB = true`, Go `IndexedDB: playwright.Bool(true)`. |
| `newContext({ storageState })` | Restores the file before the first request, IndexedDB included when the file has it. No option needed on this side. In Go: `context.SetStorageState(path)`. |
| A store of non-JSON values | `Date`, `Uint8Array`, `BigInt`, `Map` and `Blob` in IndexedDB, to see which ones survive the trip through JSON. |
| Two sessions | The first browser is gone before the second starts; only the files carry the login. |

## How it works

1. Session 1: set a session cookie on httpbin.org, store a theme in localStorage and write the Firebase-shaped user and the typed values to IndexedDB.
2. Save the state twice: once with the defaults, once with IndexedDB.
3. Close the browser. Session 2, a new browser: restore each file into its own context.
4. In each, check the cookie the site receives, the localStorage value and whether the app would find its signed-in user — then which IndexedDB value types came back as what.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-12):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| State file | File size (bytes) | Cookie sent | localStorage | Firebase user in IndexedDB | App signed in |
|---|---|---|---|---|---|
| cookies + localStorage | 403 | true | dark | — | false |
| with IndexedDB | 2551 | true | dark | alice@example.com | true |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The default file brought back the cookie and localStorage but not the user** — a Firebase app would show its sign-in form. With `indexedDB: true` the user and its tokens came back too, so the app would be signed in.
- **Only saving needs the option.** `newContext({ storageState })` restores whatever IndexedDB is in the file. The file grew from a few hundred bytes to about 1–2.5 KB here (Node writes it indented) — and it now holds the refresh token in plain text, so guard it like a password.
- **Not every value survives:** `Date`, `Uint8Array` and `BigInt` came back as themselves; a `Map` and a `Blob` came back as empty objects `{}`. Check what the app you depend on stores before trusting a restored login.
- **In Go, restore with `SetStorageState`:** playwright-go 0.6000's `StorageStatePath` on `NewContext` drops IndexedDB (it parses the file into a typed struct with no field for it — tried here, the app came back signed out). `context.SetStorageState(path)` passes the file to Playwright untouched, IndexedDB included. The other four clients restore it from `newContext` as is.
