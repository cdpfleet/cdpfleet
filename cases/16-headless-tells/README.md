# What headless gives away, what headful fixes, and what neither does

**Medium** · 2026-10-04 · Chromium, Patchright, Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/headless-tells)

> Twelve classic detection signals read from inside the page in headless Chromium, headful Chromium, Patchright and Camoufox. Headful removes the obvious tells; the GPU and the host still talk.

## The problem

Detection scripts don't need your TLS fingerprint to know a browser is automated: a handful of JavaScript properties give headless browsers away in a millisecond — `HeadlessChrome` in the user agent, an empty plugin list, a notification permission that says *denied* while the permissions API says *prompt*, a zero-sized outer window, `navigator.webdriver`. Running headful (the fleet's default, 2 threads) is the usual fix. But which signals does it actually fix, what do the stealth builds change on top, and what still identifies a server-side browser?

## What we used, and why

| What | Why |
|---|---|
| `chromium` with `headless: "new"` and with `headless: false` | The same build, with and without a display, so the difference is the display alone. |
| `patchright`, headful | Playwright's Chromium with the automation leaks patched — what changes beyond headful. |
| `camoufox`, headful, `os: "windows"` | A build that spoofs the device at the C++ level: screen, GPU, cores and all. |
| `main_world_eval: true` and the `mw:` prefix | Camoufox runs `page.evaluate` in an isolated world; the page's own world is what a detection script sees. |
| `navigator.permissions.query` vs `Notification.permission` | The classic headless contradiction: *prompt* from one API, *denied* from the other. |
| `WEBGL_debug_renderer_info` | The unmasked GPU renderer string — the signal that outlives headful. |

## How it works

1. Launch the four browsers through the same proxy.
2. Open a page and read twelve properties the way a detection script would: user agent, `navigator.webdriver`, plugins, the notification contradiction, outer window size, screen, WebGL renderer, cores, memory, languages, `window.chrome`.
3. Count which of the five hard tells fire for each browser and print one row per browser.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-04):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Threads | Tells fired | Headless in UA | webdriver | Plugins | Notification contradiction | Screen | WebGL renderer | Cores | Memory (GB) | window.chrome |
|---|---|---|---|---|---|---|---|---|---|---|---|
| Chromium, headless | 1 | ua_says_headless, notification_mismatch | true | false | 0 | true | 1280x720 | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) | 64 | 32 | false |
| Chromium, headful | 2 | none | false | false | 5 | false | 1280x720 | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) | 64 | 32 | true |
| Patchright, headful | 2 | none | false | false | 5 | false | 1280x720 | — | 64 | 32 | true |
| Camoufox, headful | 2 | none | false | false | 5 | false | 2560x1440 | ANGLE (NVIDIA, NVIDIA GeForce GTX 980 Direct3D11 vs_5_0 ps_5_0), or similar | 16 | — | false |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headless Chromium fires two tells on arrival:** `HeadlessChrome` in the user agent and the notification contradiction (`Notification.permission` is *denied* while `permissions.query` says *prompt*). It also has zero plugins and no `window.chrome`.
- **Headful fixes all of that with the same build:** no `Headless` in the UA, five plugins, consistent permissions, `window.chrome` present — one extra thread buys it. `navigator.webdriver` is `false` in every build on the fleet, headless included.
- **What headful doesn't fix is the hardware:** both Chromium modes report the same `SwiftShader` software renderer and the server's 64 cores and 32 GB — a desktop that doesn't exist. Sites that score the GPU string see it either way.
- **Patchright, headful, reports no WebGL renderer at all** on the fleet today (no WebGL context is created), which bot.sannysoft.com marks red; its headless mode has WebGL. We are looking into it. Everything else matches headful Chromium.
- **Camoufox replaces the hardware story:** a consumer GPU string, a desktop-sized screen, 8–32 cores, no `deviceMemory` (Firefox never had it) and no `window.chrome` — consistent with the Firefox-on-Windows identity it claims.
