# Inspect and edit the cookie jar of a remote browser — and why WebKit refuses cookies on httpbin.org

**Easy** · 2026-10-17 · Chromium, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/inspect-and-edit-the-cookie-jar)

> Read the cookies a page would send, change a value and an expiry, delete one by name and carry the login into a new session as about 150 bytes of JSON. Then set cookies three ways on three hosts in Chromium, Firefox and WebKit: WebKit refuses every cookie on httpbin.org and postman-echo.com, which are on the Public Suffix List.

## The problem

A scraper often needs to look inside the cookie jar: is the login cookie there, when does it expire, what did the consent banner set? Sometimes it has to change one — extend a session, switch a preference — or move a login from one run to the next without keeping a whole browser profile. Playwright's context cookie API does all of this over the remote connection. And when a cookie that works in Chromium silently doesn't stick in WebKit, it helps to know why.

## What we used, and why

| What | Why |
|---|---|
| `context.cookies(url)` | The cookies that would be sent to that URL — domain, path and secure flag applied. Without a URL it returns every cookie in the context. |
| `context.addCookies([...])` | Adds cookies, or replaces one with the same name, domain and path: the way to edit a value or an expiry. `expires: -1` is a session cookie. |
| `context.clearCookies({ name })` | Deletes only the matching cookies (`name`, `domain` and `path` filters, Playwright 1.43+); without a filter it empties the jar. |
| `www.httpbin.org/cookies` | Echoes the cookies the server received, so every change is checked from the site's side too. |
| `proxy` | Your own proxy; a few KB of traffic. |

## How it works

1. Chromium: the site sets `session_token` and `theme` (session cookies); a cookie for example.com is added too. Read the whole jar, then only the site's cookies.
2. Edit: give `session_token` an expiry a week out and change `theme` to `light` by adding them again; the site echoes what it received.
3. Delete `theme` by name, save the site's cookies as JSON and close the session.
4. Launch a new session, add the saved cookies to an empty context and ask the site what it sees.
5. In Chromium, Firefox and WebKit, set a cookie three ways — `addCookies`, a server `Set-Cookie` and `document.cookie` — on httpbin.org, www.httpbin.org and postman-echo.com, and read back what was kept.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-17):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Engine | Host | addCookies | Set-Cookie | document.cookie | Server received |
|---|---|---|---|---|---|
| chromium | httpbin.org | kept | kept | kept | 3 of 3 |
| chromium | www.httpbin.org | kept | kept | kept | 3 of 3 |
| chromium | postman-echo.com | kept | kept | kept | 3 of 3 |
| firefox | httpbin.org | kept | kept | kept | 3 of 3 |
| firefox | www.httpbin.org | kept | kept | kept | 3 of 3 |
| firefox | postman-echo.com | kept | kept | kept | 3 of 3 |
| webkit | httpbin.org | refused | refused | refused | 0 of 3 |
| webkit | www.httpbin.org | kept | kept | kept | 3 of 3 |
| webkit | postman-echo.com | refused | refused | refused | 0 of 3 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **`context.cookies(url)` is the filter you want:** the jar held three cookies, the site's URL matched two. Session cookies come back with `expires: -1`.
- **Editing is adding again:** the same name, domain and path replace the cookie — the server saw `theme=light` and the token now expires in 7 days. `clearCookies({ name: 'theme' })` removed that one cookie and left the rest.
- **A login travels as plain JSON:** 150–180 bytes of `context.cookies(url)` output (depending on the language's serializer), added to an empty context in a new session on another browser, and the site saw `session_token` again. That is the lightest way to carry state; [storageState](https://cdpfleet.com/docs/cases/persist-login) adds localStorage, and a [persistent profile](https://cdpfleet.com/docs/cases/persistent-profiles) keeps everything on our side.
- **WebKit refuses every cookie on httpbin.org and postman-echo.com** — through `addCookies`, `Set-Cookie` and `document.cookie` alike — and keeps all three on www.httpbin.org. Both domains are on the [Public Suffix List](https://publicsuffix.org/) (its private section), and WebKit on Linux won't store a cookie for a public suffix. Chromium and Firefox kept host-only cookies there.
- So if cookies work in Chromium but vanish in WebKit, check the domain against the Public Suffix List before blaming your code — and test cookie logic against a normal host such as www.httpbin.org.
