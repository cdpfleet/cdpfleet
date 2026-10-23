# Installed fonts as a fingerprint: what a width probe sees on each Camoufox persona

**Medium** · 2026-10-16 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/fonts-fingerprint)

> The classic font probe — 46 families measured against the generic fonts — on Camoufox's Windows, macOS and Linux personas, with extra `fonts` and with `custom_fonts_only`. The list follows the OS and is identical every launch; `fonts` can only add what the server has; and the three generic families render in one face.

## The problem

Which fonts a machine has is one of the oldest fingerprinting signals: a script sets a test string in each candidate family and measures it. A Windows user agent with macOS fonts, or a machine with three fonts in total, stands out. Camoufox builds the font list from its OS persona and lets you add to it or replace it. What does a probe actually see, is it the same every launch, and what do `fonts` and `custom_fonts_only` change?

## What we used, and why

| What | Why |
|---|---|
| A width probe over 46 families | Each family is set with `monospace`, `serif` and `sans-serif` as fallback; if any measurement differs from the generic family alone, the family counts as installed. This is how common fingerprinting libraries do it. |
| `os: "windows" \| "macos" \| "linux"` | The persona decides which families the page can see. |
| `fonts: [...]` | Families exposed in addition to the persona's list. |
| `custom_fonts_only: true` | Expose only the families in `fonts`. |
| A hash of the detected list | One value per launch that a site could store and compare. |

## How it works

1. Launch Camoufox six times one after another: Windows twice, macOS, Linux, Windows with three extra fonts, and Windows with a custom list only.
2. In each, run the width probe over the same 46 candidates and record the detected families, their hash, and whether the three generic families measure the same.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-22):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | navigator.platform | Detected (of 46) | List hash | Generics identical | Families found |
|---|---|---|---|---|---|
| os: windows (launch 1) | Win32 | 21 | dd78c7d96b | yes | Arial, Arial Black, Calibri, Cambria, Candara, Comic Sans MS, Consolas, Constantia, Corbel, Courier New, Georgia, Impact, Lucida Console, Lucida Sans Unicode, Microsoft Sans Serif, Palatino Linotype, Segoe UI, Tahoma, Times New Roman, Trebuchet MS, Verdana |
| os: windows (launch 2) | Win32 | 21 | dd78c7d96b | yes | Arial, Arial Black, Calibri, Cambria, Candara, Comic Sans MS, Consolas, Constantia, Corbel, Courier New, Georgia, Impact, Lucida Console, Lucida Sans Unicode, Microsoft Sans Serif, Palatino Linotype, Segoe UI, Tahoma, Times New Roman, Trebuchet MS, Verdana |
| os: macos | MacIntel | 23 | 23e513dd7a | yes | Arial, Arial Black, Comic Sans MS, Courier New, Georgia, Impact, Microsoft Sans Serif, Tahoma, Times New Roman, Trebuchet MS, Verdana, American Typewriter, Avenir, Avenir Next, Futura, Geneva, Gill Sans, Helvetica, Helvetica Neue, Lucida Grande, Menlo, Monaco, Optima |
| os: linux | Linux armv81 | 3 | 5bebd342f7 | yes | Arial, Courier New, Times New Roman |
| windows + fonts | Win32 | 23 | db08511274 | yes | Arial, Arial Black, Calibri, Cambria, Candara, Comic Sans MS, Consolas, Constantia, Corbel, Courier New, Georgia, Impact, Lucida Console, Lucida Sans Unicode, Microsoft Sans Serif, Palatino Linotype, Segoe UI, Tahoma, Times New Roman, Trebuchet MS, Verdana, Helvetica Neue, Menlo |
| windows + fonts + custom_fonts_only | Win32 | 3 | 0d966331f2 | yes | Calibri, Consolas, Segoe UI |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **The font list follows the persona and doesn't change between launches:** two Windows launches produced the same 21 families and the same hash. Unlike the canvas, which Camoufox re-seeds every launch ([case 49](https://cdpfleet.com/docs/cases/camoufox-same-device)), the font list is a stable trait of the persona.
- **Each OS shows its own families:** Windows had Segoe UI, Calibri and Consolas (21 found); macOS had Helvetica Neue, Menlo, Monaco and Avenir (23 found); Linux only Arial, Courier New and Times New Roman. That Linux list is short, and a short list is itself unusual.
- **`fonts` adds only what the server has:** Menlo and Helvetica Neue appeared on top of the Windows list; Noto Sans, which we also listed, never did. What the server has grew with Camoufox 156 (fleet default from 2026-10-21): measured separately on 2026-10-22, Fira Code, Roboto and JetBrains Mono now appear when listed, which they didn't on 152.
- **`custom_fonts_only` replaces the list with the installed families you name:** of Calibri, Consolas, Segoe UI and Noto Sans, the probe found the first three. (On Camoufox 152 Calibri vanished here, because it became the face the generic families fall back to; on 156 it stays visible.)
- **The three generic families render in one face.** In every setup `serif`, `sans-serif` and `monospace` measured exactly the same. On a normal desktop they are different faces, so a script that compares `serif` with `monospace` can see this. On Camoufox 152 that shared face also hid Arial from the Windows persona; on 156 Arial is detected on every persona.
