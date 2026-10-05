# Four ways to make a request, four different visitors: goto, in-page fetch, page.request and your own client

**Medium** · 2026-10-05 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/proxy-paths)

> The same URL fetched from the browser, from a script inside the page, from Playwright's request API and from your machine — and what the server saw each time: exit IP, user agent, HTTP version and TLS fingerprint.

## The problem

Playwright gives you several ways to fetch a URL once a browser is running, and they look interchangeable: `page.goto`, a `fetch()` inside `page.evaluate`, `page.request.get`, or plain HTTP from your script with the browser's cookies copied over. They are not interchangeable. Each runs on a different machine or in a different network stack, so the server sees a different IP, a different TLS handshake and sometimes a different HTTP version — and an anti-bot system compares exactly those things with the user agent. Which paths go through your proxy, which look like the browser on the wire, and which only pretend to?

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session; the question is about request paths, not the browser. |
| `https://tls.peet.ws/api/all` | Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4) — the facts a server-side bot check keys on. |
| `page.goto` and `fetch()` in `page.evaluate` | Both run in the browser: its proxy, cookies, headers and TLS stack. |
| `page.request.get` | Playwright's HTTP client (Node.js), run by the Playwright server next to the browser; it copies the browser's user agent and cookies. |
| `fetch()` in the script itself | Your machine, your runtime's TLS, no proxy — the reference point. |
| `http://ip-api.com/json/<ip>?fields=hosting` | Whether each exit is a residential ISP (the proxy) or a hosting provider (a server). |

## How it works

1. Launch one headless Chromium session through the proxy.
2. Fetch the fingerprint endpoint four ways: a navigation, a same-origin `fetch` from inside the page, `page.request.get`, and a plain `fetch` from the script.
3. For each: record exit IP and its type, user agent, HTTP version, JA4, number of TLS extensions and whether the handshake was resumed (`pre_shared_key`).
4. Compare the cipher-suite part of JA4 with the navigation's to tell "same TLS stack" from "same connection".

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-05):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Path | Runs on | Exit | HTTP | JA4 | TLS ext. | Resumed TLS | Browser's TLS stack | User agent |
|---|---|---|---|---|---|---|---|---|
| page.goto | the browser | residential | h2 | t13d1516h2_8daaf6152771_d8a2da3f94cd | 18 | false | true | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/148.0.7778.96 Safari/537.36 |
| fetch() in page.evaluate | the browser | residential | h2 | t13d1517h2_8daaf6152771_b6f405a00624 | 19 | true | true | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/148.0.7778.96 Safari/537.36 |
| page.request.get | Playwright server | residential | HTTP/1.1 | t13d5211_b262b3658495_8e6e362c5eac | 11 | false | false | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/148.0.7778.96 Safari/537.36 |
| fetch() in your script | your machine | datacenter | HTTP/1.1 | t13d5911h1_a33745022dd6_1f22a2ca17c4 | 11 | false | false | node |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Only the browser paths are the browser:** `page.goto` and an in-page `fetch` share Chrome's cipher suites, HTTP/2 and user agent. Their JA4 differs by one extension — the fetch reused the TLS session (`pre_shared_key`), a normal browser behaviour — so compare the cipher hash, not the whole string, when checking "is this the same stack".
- **`page.request` goes through your proxy but is not the browser:** the exit was residential (the fleet runs the request through the session's proxy), the user agent is Chrome's — and the handshake is Node.js's: HTTP/1.1, 11 extensions, a JA4 that no Chrome ever sends. A Chrome user agent on a Node TLS stack is the textbook bot signature.
- **Your own client is a third identity:** a datacenter exit, your runtime's TLS and user agent (`node`, `python-requests/…`, `Java-http-client/…`, `Go-http-client/2.0`, none at all from .NET), HTTP/1.1 from Node, Python and C#, HTTP/2 from Java and Go. Copying the browser's cookies into it ties that identity to the browser's session.
- **Exit IPs rotate per connection on a rotating residential proxy:** four requests, four exits, all residential except your own. Use sticky ports when a session must keep one IP; see [three visitors, one thread](https://cdpfleet.com/docs/cases/contexts-not-sessions).
- **Rule of thumb:** data the site should see you fetch as a browser — fetch it in the page (`page.evaluate(() => fetch(url))`, as in [infinite scroll, two ways](https://cdpfleet.com/docs/cases/infinite-scroll)). Use `page.request` for things the site never fingerprints, or not at all.
