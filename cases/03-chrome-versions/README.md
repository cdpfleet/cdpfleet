# Stable, beta, dev and an older major: four Chromes side by side

**Easy** · 2026-09-30 · Google Chrome · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/chrome-versions)

> Pick a channel or pin a previous major, read the live catalog instead of hard-coding versions, and see what actually differs on the wire.

## The problem

A site breaks on the new Chrome, or you need to test the next release before your users get it, or a target only trusts the current version. cdpfleet runs Chrome stable, beta and dev, plus previous majors. Which options select which build — and does the version change the fingerprint a site sees?

## What we used, and why

| What | Why |
|---|---|
| `GET /api/public/browsers` | The live catalog: which channels and previous majors exist right now. Majors rotate, so read it instead of hard-coding numbers. |
| `channel: "beta"` / `"dev"` | Selects a pre-release build. Omit it (or send `"stable"`) for stable. |
| `version: "152"` | Pins a retained previous major. A major the fleet doesn't have returns `503 version_unavailable` instead of silently running another. Don't combine it with a pre-release channel (that's a `400`). |
| `headless: false` | A real (virtual) display, so the user agent reads `Chrome/…`, not `HeadlessChrome/…`. |

## How it works

1. Read the catalog and build one variant per listed Chrome build.
2. Launch all variants at once — each is its own session.
3. In each, compare `browser.version()` with the catalog, and read the user agent, `sec-ch-ua` and fingerprints from tls.peet.ws.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-09-30):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Variant | browser.version() | User agent | sec-ch-ua | JA4 |
|---|---|---|---|---|
| channel stable | 153.0.8010.36 | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/203.0.113.1 Safari/537.36 | "Google Chrome";v="153", "Not_A Brand";v="8", "Chromium";v="153" | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| channel beta | 155.0.8059.12 | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/203.0.113.2 Safari/537.36 | "Google Chrome";v="155", "Chromium";v="155", "Not(A:Brand";v="24" | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| channel dev | 156.0.8072.0 | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/203.0.113.3 Safari/537.36 | "Not:A-Brand";v="8", "Chromium";v="156", "Google Chrome";v="156" | t13d1517h2_8daaf6152771_cb7bf5808d99 |
| version 152 | 152.0.7977.82 | Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/203.0.113.4 Safari/537.36 | "Chromium";v="152", "Not?A_Brand";v="24", "Google Chrome";v="152" | t13d1517h2_8daaf6152771_cb7bf5808d99 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Every channel got exactly the build the catalog lists,** to the last digit. **A pinned major gives you that major;** the patch level can differ between servers (the catalog shows the most common one).
- **The user agent only carries the major** (`Chrome/155.0.0.0`): Chrome's user-agent reduction. The full version is in `sec-ch-ua-full-version-list` for sites that ask.
- **`sec-ch-ua` changes per major** — including its "GREASE" brand (`Not_A Brand`, `Not(A:Brand`, `Not:A-Brand`…), which Chrome rotates on purpose. Copying one version's header onto another is detectable.
- **TLS (JA4) and HTTP/2 fingerprints are identical from 152 to 156** — so a pinned major doesn't cost you anything on the network side.
