# Three visitors, one thread: browser contexts instead of sessions

**Easy** · 2026-10-05 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/contexts-not-sessions)

> Cookies, storage, locale and clock isolated per persona inside a single browser session — what a context gives you for free, what it shares, and the proxy surprise in the exit IPs.

## The problem

Running several identities — accounts, A/B variants, regional visitors — is usually done with one browser per identity, which on a fleet means one session and at least one thread each. Playwright's browser contexts promise the same isolation inside one browser: separate cookie jars, storage, permissions, locale and timezone, like incognito windows that don't know about each other. Is the isolation real when the browser is remote, what do the contexts still share, and what does it cost?

## What we used, and why

| What | Why |
|---|---|
| One `chromium` session, `headless: "new"` | All three personas live in it: 1 thread in total. |
| `browser.newContext({ locale, timezoneId })` | Each context gets its own language and clock — New York, São Paulo, Tokyo — without touching the others. |
| `context.addCookies` and `localStorage` | A login token per persona, planted before the first page; storage written from the page. |
| Reading everything after all three exist | A leak between contexts would only show once the others have written their state. |
| `http://ip-api.com/json` from each context | Where each persona's traffic exits — all three use the session's proxy. |

## How it works

1. Launch one headless Chromium session; note the `weight` from the launch response.
2. For each persona, create a context with its locale and timezone, plant a session cookie for example.com, open the page and write the persona's name to `localStorage`.
3. With all three open, read back from each: `document.cookie`, the storage value, `navigator.language`, the resolved timezone, a fixed UTC instant as local time.
4. Fetch the exit IP from each context and print one row per persona.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-05):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Persona | Threads | document.cookie | localStorage owner | Language | Timezone | 12:00 UTC shown as | Exit IP |
|---|---|---|---|---|---|---|---|
| alice | 1 | session=alice-token | alice | en-US | America/New_York | 8:00:00 AM | 203.0.113.1 |
| bruno | 1 | session=bruno-token | bruno | pt-BR | America/Sao_Paulo | 09:00:00 | 203.0.113.2 |
| chie | 1 | session=chie-token | chie | ja-JP | Asia/Tokyo | 21:00:00 | 203.0.113.3 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Isolation holds over the remote connection:** each persona saw only its own cookie and its own storage value — no leaks between contexts, exactly as in a local browser.
- **Locale and clock are per context:** the same instant read as 8:00 AM in New York, 09:00 in São Paulo and 21:00 in Tokyo, with matching `navigator.language` — three regional visitors from one browser.
- **It cost one thread.** Three sessions would have cost three (six, headful). Contexts are the cheap way to multiply identities when they can share a browser build and a proxy.
- **What they share: the browser and its proxy.** All contexts go out through the session's proxy — and because ours is a rotating residential proxy, each new connection got a different exit IP (three personas, three IPs). If a persona needs a stable IP, use a sticky proxy port; if two personas must exit from different countries, that is where sessions (with different proxies, or `proxy_rules`) come in.
- **Also shared: the fingerprint.** Contexts can change user agent, viewport, locale and timezone, but not the engine, its TLS stack or the GPU string. For identities that must differ at that level, see [Camoufox](https://cdpfleet.com/docs/browsers/camoufox).
