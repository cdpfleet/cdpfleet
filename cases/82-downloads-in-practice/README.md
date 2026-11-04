# Kept downloads in practice: a person in Launch interactive and an AI agent over MCP

**Easy** · 2026-11-04 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/downloads-in-practice)

> Files nobody's code was waiting for: a person pastes file links into a Launch interactive session, an AI agent clicks a download and opens a file link through the hosted MCP server. Both sessions run with "downloads": true; afterwards the files are listed, fetched and checked byte for byte against the originals — and the program times how long after the session ends they show up.

## The problem

Playwright's `download.saveAs()` only works while your code is connected and waiting for the file. Many downloads happen when it isn't: a person clicking "Export" in live view or in a Launch interactive session, an AI assistant working through the MCP server, a script that already disconnected. With `"downloads": true` the session keeps every file it downloads for 2 days. This case does both hand-driven and agent-driven downloads for real, then collects the files the way you would afterwards: list them, fetch them through their private links, and check that the bytes are the originals.

## What we used, and why

| What | Why |
|---|---|
| `"downloads": true` | Keeps every file the session downloads, for 2 days (paid plans). In MCP it is the `downloads` argument of `launch_browser`. |
| `start_url` + `live_view` | Launch interactive from code: the server opens the page itself, no client connects, and a control live link lets a person drive it. |
| The live link's `navigate` message | What the viewer's address bar sends. The person here is a scripted stand-in that pastes two file links. |
| `DELETE /v1/me/threads/{id}` | Ends the session — what the dashboard does when you close a Launch interactive viewer. |
| `https://cdpfleet.com/mcp` | The hosted MCP server, with the API key as `Authorization: Bearer`, as in [MCP in action](https://cdpfleet.com/docs/cases/mcp-in-action): `launch_browser`, `navigate`, `click`, `evaluate`, `close_browser`, then the `downloads` tool. |
| `GET /v1/me/downloads?session_id=…`, `GET /v1/me/downloads/{id}/url` | The session's kept files with `bytes`, `sha256` and `source_url`, and a private link per file (15 minutes). |
| httpbingo.org `/bytes/{n}?seed=…` | Test files whose bytes depend only on the URL, so each kept copy can be compared with the original fetched directly. |

## How it works

1. Launch headless Chromium with `downloads`, `live_view` and `start_url: https://httpbingo.org/`, and no client. Get a control live link and open it.
2. The person pastes two file links into the address bar (200 KB and 64 KB), 3 seconds apart, then closes the viewer; the session is ended with `DELETE /v1/me/threads/{id}`.
3. Poll `GET /v1/me/downloads?session_id=…` until both files are listed, fetch each through `GET /v1/me/downloads/{id}/url`, and compare size and sha256 with the list and with the original.
4. Over MCP: launch Chromium with `downloads: true`, open httpbingo.org, click its `/bytes/:n` example link, `navigate` to a 100 KB file link, read where the page is, and `close_browser` two seconds later.
5. Call the `downloads` tool until it lists both files, fetch them through the links it returns, and check them against the account API's list and the original.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-11-04):

- [Node.js](node.mjs) — npm install ws @modelcontextprotocol/sdk@1.32.2 && node node.mjs
- [Python](main.py) — pip install aiohttp mcp==2.3.0 && python main.py
- [Java](Main.java) — Maven with io.modelcontextprotocol.sdk:mcp:2.1.0 and com.google.code.gson:gson:2.11.0 (the templates/java pom plus the MCP SDK), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package ModelContextProtocol --version 2.3.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/gorilla/websocket@v1.5.3 && go run . (MCP as standard-library JSON-RPC)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Who downloaded | How | File | Listed after the session ended | Fetched copy | Measured |
|---|---|---|---|---|---|
| a person, Launch interactive | pasted into the address bar | 204800 (200 KB), sha256 4b2aa65bcf5e… | 309 ms (account API) | matches the list, identical to the original | 2026-11-04 |
| a person, Launch interactive | pasted into the address bar | 65536 (64 KB), sha256 959b88aba150… | 309 ms (account API) | matches the list, identical to the original | 2026-11-04 |
| an AI agent, hosted MCP | navigate to the file link | 102400 (100 KB), sha256 69c384ebb4f1… | 741 ms (downloads tool) | matches the list, identical to the original | 2026-11-04 |
| an AI agent, hosted MCP | click on a link | 1024 (1 KB), sha256 3f15a7590652… | 741 ms (downloads tool) | matches the list (random bytes: no original to compare) | 2026-11-04 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The files are there within about a second of the session ending.** On 2026-11-04, across five runs (one per language), the person's two files were listed by `GET /v1/me/downloads` 0.3–0.9 s after `DELETE /v1/me/threads/{id}` returned, and the agent's two files were in the `downloads` tool's answer 0.7–1.2 s after `close_browser` returned. The docs promise "within about a minute"; a short poll is enough.
- **Ask the `downloads` tool again.** Called straight after `close_browser`, the tool answered "No kept files for session … yet" in all five runs; the next call half a second later (the third, in one run) listed both files with a private link each. The answer says the files appear within about a minute — an agent that gives up after the first reply misses them.
- **The kept copies are the originals.** All 20 files (four per run) fetched through their private links had exactly the listed size and sha256, served as `application/octet-stream`. The three seeded files — 200 KB and 64 KB from the person, 100 KB from the agent — were byte-identical to the same URLs downloaded directly, in every run. Check the `sha256` the list gives you; it is the file's.
- **Nobody's code has to be there.** The Launch interactive session never had a client: the person pasted two file links into live view's address bar, the tab stayed on httpbingo.org, and both files were kept. The dashboard's Launch interactive uses the code generator's settings, where `downloads` is a toggle.
- **For an agent, "Download is starting" isn't a failure.** `navigate` to a file link came back as an error (`page.goto: Download is starting`) in every run, yet the file was downloaded and kept like the one the agent clicked. Tell your agent that, or have it `click` the link instead.
