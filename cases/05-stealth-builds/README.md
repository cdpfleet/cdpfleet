# Three Chromium builds against common bot checks, headless and headful

**Hard** · 2026-09-30 · Chromium, Google Chrome, Patchright · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/stealth-builds)

> chromium, Chrome and Patchright through the checks detection scripts run first — and why headless mode is the loudest signal.

## The problem

cdpfleet offers several Chromium builds with different anti-detection work. Which of them pass the checks a typical detection script runs in its first milliseconds — `navigator.webdriver`, the user agent, plugins, WebGL renderer, permission consistency, window chrome? And how much does headless mode change the picture?

## What we used, and why

| What | Why |
|---|---|
| `chromium` | Plain open-source Chromium: the baseline. |
| `chrome` | Real Google Chrome (stable). |
| `patchright` | Chromium with Patchright's automation-hiding patches. |
| `headless: true` / `false` | Headless costs 1 thread, headful (a virtual display) 2. Each build runs both. |
| A page `<script>` via `page.route` | The checks run as a real page script on an https origin, the way a website runs them — not through `page.evaluate`. |

## How it works

1. Launch all six combinations (3 builds × headless/headful) at once.
2. Serve a small detection page with `route.fulfill` on an https URL and let its script run.
3. Collect what the script found.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-15):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Build | Mode | webdriver | Headless in UA | Plugins | Permission mismatch | WebGL renderer |
|---|---|---|---|---|---|---|
| chromium | headless | false | true | 0 | true | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |
| chromium | headful | false | false | 5 | false | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |
| chrome | headless | false | true | 5 | false | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |
| chrome | headful | false | false | 5 | false | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |
| patchright | headless | false | true | 0 | true | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |
| patchright | headful | false | false | 5 | false | ANGLE (Google, Vulkan 1.3.0 (SwiftShader Device (Subzero) (0x0000C0DE)), SwiftShader driver) |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **No build exposes `navigator.webdriver`** — in either mode.
- **Headless is the loudest tell:** chromium, Chrome and Patchright put `HeadlessChrome` in the user agent when headless; chromium and Patchright also report 0 plugins and a notification-permission mismatch. The same builds headful pass those checks. Budget 2 threads for headful when a site checks.
- **The GPU is the next tell:** chromium, Chrome and Patchright all report Google SwiftShader (a software renderer — "this is a VM") — none of them reports a real-looking GPU.
- These are first-line checks. Serious vendors add many more (canvas, audio, timing, behaviour); treat this as a floor, not a guarantee.
