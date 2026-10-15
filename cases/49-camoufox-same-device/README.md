# The same Camoufox device across relaunches: what a persistent profile keeps, and what it doesn't

**Medium** · 2026-10-15 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/camoufox-same-device)

> A Camoufox profile brought back the same screen, cores, GPU and user agent — and the login — on every relaunch, where launches without one got a new device each time. Two things still change per launch: the canvas noise, and the timezone if the exit IP moves.

## The problem

Accounts that are used for weeks get flagged when every visit looks like a different computer. Camoufox builds a believable persona on each launch — which is exactly the problem for a long-lived account: a new screen, GPU and CPU count every time. Does a persistent profile really bring the same device back, along with the login, and what does a fingerprinting script still see change?

## What we used, and why

| What | Why |
|---|---|
| `"profile": "…"` | The first launch creates the profile — Camoufox stores the persona it generated with the cookies and storage; later launches restore it. Work in `browser.contexts()[0]`. |
| `"os": "windows"` | Only used when the profile is created; later launches keep the stored persona whatever you pass. |
| `PROXY_URL_US` | One country for every launch: Camoufox sets timezone and locale from the exit IP. |
| A small fingerprinting script | User agent, platform, CPU cores, screen, WebGL renderer and a canvas hash — read on httpbin.org, plus a cookie and a localStorage entry as the "login". |
| `GET` / `DELETE /v1/me/profiles` | Wait until each save has landed before the next launch, and delete the profile at the end. |

## How it works

1. Launch Camoufox four times with one new profile; on the first launch set a session cookie and a localStorage value; on each launch read the fingerprint (drawing the canvas twice).
2. Wait for the profile's new version to be saved between launches.
3. Launch twice more without a profile, for contrast, then delete the profile.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-15):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL`, `PROXY_URL_US` (see [cases/README.md](../README.md#environment)).

## What we got

| Launch | Device hash | Screen · cores · GPU | Canvas hash (same twice in session?) | Timezone | Login kept |
|---|---|---|---|---|---|
| profile, 1 | 9b4c5e3d22 | 2176x1224 · 24 · ANGLE (Intel, Intel(R) HD Graphics Direct3D11 vs_5_0 ps_5_0), or similar | 2aa3626347ab (yes) | America/New_York | set |
| profile, 2 | 9b4c5e3d22 | 2176x1224 · 24 · ANGLE (Intel, Intel(R) HD Graphics Direct3D11 vs_5_0 ps_5_0), or similar | eee3aa52d041 (yes) | America/Chicago | yes |
| profile, 3 | 9b4c5e3d22 | 2176x1224 · 24 · ANGLE (Intel, Intel(R) HD Graphics Direct3D11 vs_5_0 ps_5_0), or similar | 3406714b0218 (yes) | America/Chicago | yes |
| profile, 4 | 9b4c5e3d22 | 2176x1224 · 24 · ANGLE (Intel, Intel(R) HD Graphics Direct3D11 vs_5_0 ps_5_0), or similar | edfb4c52ec36 (yes) | America/New_York | yes |
| no profile, 1 | e57979ea7d | 2176x1224 · 8 · ANGLE (NVIDIA, NVIDIA GeForce GTX 980 Direct3D11 vs_5_0 ps_5_0), or similar | 3f49470cb734 (yes) | America/New_York | — |
| no profile, 2 | de4852f49f | 1920x1080 · 8 · ANGLE (NVIDIA, NVIDIA GeForce GTX 980 Direct3D11 vs_5_0 ps_5_0), or similar | 42bb3f9ae210 (yes) | America/New_York | — |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The device comes back:** all four launches with the profile reported the same user agent, platform, CPU cores, screen and WebGL renderer — one device hash. Launches without a profile drew a new persona each time — from a limited set of common real configurations, so while building this case one unrelated launch happened to match the profile's screen, cores and GPU exactly. A profile is what makes the same device certain.
- **So does the login:** the cookie and localStorage value set on the first launch were there on the second, third and fourth.
- **The canvas changes on every launch, even with the profile:** the same drawing gave the same hash twice within a session, but a different one in each new session. Camoufox re-seeds its canvas noise per launch, so a site that stores a canvas hash sees it change between visits — judge for yourself whether your target compares canvases across sessions.
- **Timezone follows the exit, not the profile:** a US exit landed in different states, so the timezone moved between US time zones from one launch to the next. Use a sticky exit — a [residential](https://cdpfleet.com/docs/proxies#residential) `-session-` token or a fixed proxy — when the account should also stay in one city.
- **Profiles are on paid plans**, 100 per account; see [Persistent profiles](https://cdpfleet.com/docs/profiles) and the [Camoufox profile recipe](https://cdpfleet.com/docs/examples#camoufox-profile).
