# What four public bot-test pages say about five fleet browsers

**Easy** · 2026-10-20 · Chromium, Google Chrome, Patchright, Camoufox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see)

> bot.sannysoft.com, CreepJS, deviceandbrowserinfo.com and the fingerprint.com playground, each read for its own verdict on headless Chromium, headful Chrome, headful Patchright, Camoufox and headless WebKit. Headless Chromium fails the most checks everywhere; Camoufox is the only setup deviceandbrowserinfo never called a bot.

## The problem

Before pointing a scraper at a protected site, people run their browser past public bot-test pages to see what gives it away. Each page checks different things and reports in its own way: a table of passed and failed checks, a headless percentage, a yes/no verdict with reasons. This case reads every page's own verdict for five setups on the fleet, so you can see which signals each one keys on. It records what the pages report on one day, not how a particular protected site will treat you.

## What we used, and why

| What | Why |
|---|---|
| `bot.sannysoft.com` | A table of classic headless and automation checks (user agent, `navigator.webdriver`, the `chrome` object, plugins, permissions, WebGL). The program counts the rows marked failed and keeps their names. |
| CreepJS (`abrahamjuliot.github.io/creepjs`) | Scores how much the browser looks headless ("like headless", "headless") and how much it looks like a stealth tool ("stealth"), as percentages. |
| `deviceandbrowserinfo.com/are_you_a_bot` | A yes/no verdict (`isBot`) with the individual signals behind it, such as a bot user agent, CDP automation, `webdriver` true. |
| `demo.fingerprint.com/playground` | The vendor's playground: a bot verdict ("Not detected", or "bad bot" with a type) and a suspect score. Only those two fields are read. |
| `page.waitForFunction()` | Each page runs its checks after load; the program waits until the verdict is on the page before reading it. |
| One context per setup, one load per page | The pages see a fresh browser each time, and no page is loaded more than once per setup. |

## How it works

1. For each of five setups, launch a session through the same proxy and open a fresh context.
2. Open each test page in turn and wait until it shows its result.
3. Read the page's own verdict from its DOM, with the same expression in every language.
4. Print one row per setup with the four verdicts.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-20):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | sannysoft: failed checks | CreepJS: like headless / headless / stealth | deviceandbrowserinfo | fingerprint.com |
|---|---|---|---|---|
| Chromium, headless | 11 of 31 (User Agent (Old), Chrome (New), Permissions (New), Plugins Length (Old), Plugins is of type PluginArray, WebGL Renderer, HEADCHR_UA, HEADCHR_CHROME_OBJ, HEADCHR_PERMISSIONS, HEADCHR_IFRAME, CHR_MEMORY) | 94% / 67% / 0% | bot (hasBotUserAgent, hasInconsistentTimingResolution, isAutomatedWithCDP, isAutomatedWithCDPInWebWorker, hasSuspiciousWeakSignals) | You are a bad bot (type: headless_chrome), suspect score 23 |
| Google Chrome, headful | 1 of 31 (WebGL Renderer) | 50% / 0% / 0% | no result (page load failed) | You are a bad bot (type: chromium_automation), suspect score 43 |
| Patchright, headful | 1 of 31 (WebGL Renderer) | 50% / 0% / 0% | bot (hasSuspiciousWeakSignals) | You are a bad bot (type: chromium_automation), suspect score 31 |
| Camoufox, defaults | 1 of 31 (Chrome (New)) | 13% / 0% / 0% | not a bot | Not detected, suspect score 9 |
| WebKit, headless | 2 of 31 (WebDriver (New), Chrome (New)) | 19% / 33% / 0% | no result (page load failed) | You are a bad bot (type: webdriver), suspect score 33 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Headless Chromium is flagged by every page:** it fails by far the most sannysoft checks (the `HeadlessChrome` user agent, the missing `chrome` object, plugins, permissions), CreepJS scores it the most headless, deviceandbrowserinfo says bot, and fingerprint.com names the type: `headless_chrome`.
- **Headful Chrome and headful Patchright pass sannysoft except its WebGL renderer check** (software rendering on a GPU-less server), but fingerprint.com still types both as `chromium_automation`. deviceandbrowserinfo flags Chrome for CDP automation (in the page and in a worker); Patchright hides those CDP signals there and is called a bot on `hasSuspiciousWeakSignals` alone. These pages look for the automation connection itself, not only headless traits ([case 43](https://cdpfleet.com/docs/cases/cdp-detectability)).
- **WebKit is caught on `navigator.webdriver`**, which Playwright's WebKit sets to true: sannysoft fails its WebDriver check and the other pages name webdriver.
- **Camoufox drew the fewest flags:** deviceandbrowserinfo found no signal in any run, CreepJS scored it least headless, and fingerprint.com reported "Not detected" in all five published runs, though an earlier run the same day (2026-10-20) got "bad bot", type `adspower`: verdicts vary from load to load. Camoufox is based on Firefox source (not affiliated with Mozilla), so sannysoft's Chrome-object check fails by design.
- These pages disagree with each other and with real protection vendors: passing all four doesn't mean a protected site will let you in, and failing one isn't always fatal. Use them to see which signals a setup gives away; see [case 16](https://cdpfleet.com/docs/cases/headless-tells) for headless tells and [case 52](https://cdpfleet.com/docs/cases/fonts-fingerprint) for fonts.
