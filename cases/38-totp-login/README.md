# Logging in through a TOTP two-factor prompt from a remote browser

**Medium** · 2026-10-12 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/totp-login)

> Generate the authenticator code in your own code, type it into a remote browser, and save the login so the next session skips the second factor.

## The problem

More and more logins ask for a one-time code from an authenticator app. An automated login can't wait for a phone, but it doesn't need one: the app only runs HMAC-SHA1 over the current 30-second window with a shared secret, which your code can do with the standard library of any language. The traps are timing — a code typed in the last seconds of its window may be checked in the next one — and logging in more often than you need to. We log into a public TOTP practice page from a cdpfleet browser.

## What we used, and why

| What | Why |
|---|---|
| [authenticationtest.com](https://authenticationtest.com/totpChallenge/) | A public practice login with e-mail, password and a TOTP code. The account, password and TOTP secret in the programs are the demo values the site itself publishes; for a real account, load the secret from an environment variable or a secret store. |
| TOTP in your own code | HMAC-SHA1 of the 30-second counter with the base32-decoded secret, truncated to 6 digits (RFC 6238). Standard library only, in all five languages: no TOTP package needed. |
| A fresh-window guard | After the form has loaded and right before typing, the program checks how much of the current window is left; with fewer than 5 seconds it waits for the next window, then generates the code. |
| A new context per attempt | Every attempt starts without cookies, so one success doesn't leak into the next attempt. |
| `context.storageState()` → `newContext({ storageState })` | Saves the signed-in cookies and restores them in a new session (a different browser, a different `sessionId`). |

## How it works

1. Launch headless Chromium through your proxy. Check the site's clock — its `Date` header should fall between your clock just before and just after the request — and record the remote browser's clock for comparison.
2. Log in with a wrong code, and with the codes from two windows ago, the previous window and the next window, each in a fresh context.
3. Log in with the current code and save the context's storage state.
4. End the session. Launch a new one and open the site with and without the saved state.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-12):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Login attempt | Window left when typed | Landed on | Accepted | Submit → result (ms) |
|---|---|---|---|---|
| wrong code | 11.4 s | /loginFail/ | false | 947 |
| code from 2 windows ago (-60 s) | 8.4 s | /loginFail/ | false | 1229 |
| code from the previous window (-30 s) | 4.5 s → waited for the next | /loginSuccess/ | true | 1105 |
| code from the next window (+30 s) | 21.1 s | /loginSuccess/ | true | 1543 |
| current code | 15.3 s | /loginSuccess/ | true | 2544 |
| new session, without saved state |  | / (signed out) | false |  |
| new session, with saved state |  | / (signed in) | true |  |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **A wrong code and the code from two windows ago were refused; the previous, current and next windows' codes were all accepted.** This site tolerates one 30-second step of clock difference in either direction. Don't count on that everywhere: RFC 6238 recommends allowing at most one step, and stricter servers allow none.
- **Generate the code where your program runs.** The code depends only on the secret and your clock; the remote browser just types it. What has to agree is your clock and the server's: in every run the site's `Date` header (sent by its CDN edge) fell between our clock just before and just after the request. The remote browser's clock was within half a second of ours, but it plays no part.
- **Don't spend the drift margin on latency.** The tolerance is there for clock differences. Make the code after the form has loaded, right before typing it, and wait for the next window when fewer than 5 seconds are left — the rows marked "waited for the next" in the table. On a server with no tolerance, a code typed in its last second fails.
- **Log in once, then reuse the session:** the saved state held a single session cookie. A new session — a different browser — was signed in with it and signed out without it. This site remembered the login, not the device, so the next session skips the second factor until the site ends the login. Some sites tie a login to the device ([persisting logins](https://cdpfleet.com/docs/cases/persist-login)), and apps that keep the login in IndexedDB need one more option ([IndexedDB login state](https://cdpfleet.com/docs/cases/indexeddb-login-state)).
- Keep the TOTP secret in a secret store, like a password: whoever holds it can produce every future code.
