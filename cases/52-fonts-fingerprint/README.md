# Installed fonts as a fingerprint: what a width probe sees on each Camoufox persona

**Medium** · 2026-10-23 · Camoufox · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/fonts-fingerprint)

> The classic font probe — 46 families measured against the generic fonts — on Camoufox's Windows, macOS and Linux personas, with extra `fonts` and with `custom_fonts_only`. The list follows the OS and repeats from launch to launch; since the fleet's 2026-10-22 Camoufox library change, `fonts` replaces the persona's list instead of adding to it, and the generic families only collapse into one face when you set `fonts`.

## The problem

Which fonts a machine has is one of the oldest fingerprinting signals: a script sets a test string in each candidate family and measures it. A Windows user agent with macOS fonts, or a machine with three fonts in total, stands out. Camoufox builds the font list from its OS persona and lets you add to it or replace it. What does a probe actually see, is it the same every launch, and what do `fonts` and `custom_fonts_only` change?

## What we used, and why

| What | Why |
|---|---|
| A width probe over 46 families | Each family is set with `monospace`, `serif` and `sans-serif` as fallback; if any measurement differs from the generic family alone, the family counts as installed. This is how common fingerprinting libraries do it. |
| `os: "windows" \| "macos" \| "linux"` | The persona decides which families the page can see. |
| `fonts: [...]` | Families to expose (documented as additions to the persona's list; see the takeaways). |
| `custom_fonts_only: true` | Expose only the families in `fonts`. |
| A hash of the detected list | One value per launch that a site could store and compare. |

## How it works

1. Launch Camoufox six times one after another: Windows twice, macOS, Linux, Windows with three extra fonts, and Windows with a custom list only.
2. In each, run the width probe over the same 46 candidates and record the detected families, their hash, and whether the three generic families measure the same.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-23):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Setup | navigator.platform | Detected (of 46) | List hash | Generics identical | Families found |
|---|---|---|---|---|---|
| os: windows (launch 1) | Win32 | 22 | 6564fcf93f | no | Arial, Arial Black, Calibri, Cambria, Candara, Comic Sans MS, Consolas, Constantia, Corbel, Courier New, Georgia, Impact, Lucida Console, Lucida Sans Unicode, Microsoft Sans Serif, Palatino Linotype, Segoe UI, Tahoma, Times New Roman, Trebuchet MS, Verdana, Helvetica |
| os: windows (launch 2) | Win32 | 22 | 6564fcf93f | no | Arial, Arial Black, Calibri, Cambria, Candara, Comic Sans MS, Consolas, Constantia, Corbel, Courier New, Georgia, Impact, Lucida Console, Lucida Sans Unicode, Microsoft Sans Serif, Palatino Linotype, Segoe UI, Tahoma, Times New Roman, Trebuchet MS, Verdana, Helvetica |
| os: macos | MacIntel | 23 | 23e513dd7a | no | Arial, Arial Black, Comic Sans MS, Courier New, Georgia, Impact, Microsoft Sans Serif, Tahoma, Times New Roman, Trebuchet MS, Verdana, American Typewriter, Avenir, Avenir Next, Futura, Geneva, Gill Sans, Helvetica, Helvetica Neue, Lucida Grande, Menlo, Monaco, Optima |
| os: linux | Linux x86_64 | 13 | f80842cabf | no | Arial, Calibri, Cambria, Courier New, Palatino Linotype, Times New Roman, Helvetica, DejaVu Sans, DejaVu Serif, Liberation Sans, Liberation Mono, Noto Sans, Ubuntu |
| windows + fonts | Win32 | 2 | 97a32c3736 | yes | Helvetica Neue, Menlo |
| windows + fonts + custom_fonts_only | Win32 | 3 | 4b85191c2b | yes | Calibri, Segoe UI, Noto Sans |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Each OS shows its own families, and the list repeats from launch to launch:** Windows had Segoe UI, Calibri and Consolas (22 found); macOS had Helvetica Neue, Menlo, Monaco and Avenir (23); Linux now shows a full Linux set, with DejaVu, Liberation, Noto Sans and Ubuntu (13–14). Measured 2026-10-23 on the fleet's Camoufox 156 with the official Camoufox library (from 2026-10-22); the earlier build showed only three Linux families. Unlike the canvas, which Camoufox re-seeds every launch ([case 49](https://cdpfleet.com/docs/cases/camoufox-same-device)), the font list is a trait of the persona.
- **On the plain personas, `serif`, `sans-serif` and `monospace` now render in different faces**, as on a normal desktop. The earlier build drew all three in one face, which a script comparing them could see.
- **`fonts` now replaces the persona's list instead of adding to it.** With `fonts: ["Menlo", "Helvetica Neue", "Noto Sans"]` on the Windows persona, the probe found only Menlo and Helvetica Neue: Segoe UI, Calibri and the rest were gone, and the three generic families collapsed into one face again (the face they fall back to is invisible to a width probe, which is likely why Noto Sans didn't show). Our docs describe `fonts` as additions; this is the behaviour measured since the library change, and it has been reported.
- **`custom_fonts_only` exposes your listed families**, minus the one the generic families fall back to: of Calibri, Consolas, Segoe UI and Noto Sans, the probe found Calibri, Segoe UI and Noto Sans, but not Consolas.
- **What to take from it:** keep the persona's own list (no `fonts`) unless you need a specific family, and if you do, list the whole set you want the site to see.
