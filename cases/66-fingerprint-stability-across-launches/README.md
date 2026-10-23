# Which fingerprints stay the same across launches: canvas, WebGL, audio and fonts on seven browsers

**Medium** · 2026-10-22 · Google Chrome, Microsoft Edge, Chromium, Brave, Camoufox, Firefox, WebKit · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/fingerprint-stability-across-launches)

> Three launches per browser, each reading a canvas hash, the WebGL renderer and a rendered WebGL image, an audio hash and a font list on the same page. Chrome, Edge, Chromium, Firefox and WebKit gave the same values every time. Brave changed its canvas and audio on every launch, and Camoufox its canvas (and usually its GPU). A profile kept Camoufox's GPU and fonts but not the canvas, and kept nothing of Brave's noise.

## The problem

Fingerprinting scripts don't only ask "is this a bot?". They also store a hash of your canvas, GPU, audio stack and fonts and compare it next time. A value that never changes ties all your sessions together; a value that changes on every visit can look like a different device each time, which is just as unusual for an account that should be one person. So which of these values does each browser keep from one launch to the next, which does it randomise, and what does a persistent profile change?

## What we used, and why

| What | Why |
|---|---|
| A small fingerprinting script | The same script in every language: a canvas drawing, the WebGL renderer string plus a hash of a rendered WebGL image, an `OfflineAudioContext` render, and a width probe over 30 font families. Canvas and audio are drawn twice to see whether a session agrees with itself. |
| `https://example.com/` | The script runs on a real page. Brave and Camoufox key their noise to the site, so on `about:blank` both looked perfectly stable while building this case. |
| Three launches per browser | Each a new session, headless, through the same proxy. Nothing else is set, so these are the defaults. |
| `"profile": "…"` | Two more launches per browser with one new profile, to see what the profile carries over. See [Persistent profiles](https://cdpfleet.com/docs/profiles). |

## How it works

1. For each of seven browsers, launch three sessions, open example.com and run the fingerprinting script.
2. Launch twice more with one new profile, waiting for the first session's profile to be saved, and run the script again.
3. Delete the profile and print every hash.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-22):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Browser | Canvas, 3 launches | WebGL renderer · image | Audio | Fonts found (of 30) | With a profile: canvas · renderer |
|---|---|---|---|---|---|
| Google Chrome | same | same · same | same | 6 (same) | kept · kept |
| Microsoft Edge | same | same · same | same | 6 (same) | kept · kept |
| Chromium | same | same · same | same | 6 (same) | kept · kept |
| Brave | 3 different | same · same | 3 different | 1 (same) | new · kept |
| Camoufox | 3 different | 3 different · same | same | 17 / 3 (2 different) | new · kept |
| Firefox | same | same · same | same | 5 (same) | kept · kept |
| WebKit | same | same · same | same | 6 (same) | kept · kept |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Chrome, Edge and Chromium gave the same canvas, WebGL image and font list on every launch, and the same values as each other.** They run on the same servers with the same software renderer (SwiftShader), so there is nothing to tell launches apart. The audio hash differs between Chromium and Chrome/Edge (different builds), but stays the same per browser. A profile changes nothing here because nothing varies.
- **Firefox and WebKit were just as stable**, each with its own values. WebKit reports "Apple GPU" and found the same fonts as the Chromium family.
- **Brave changed its canvas and audio hashes on every launch, and a profile didn't keep them.** Within one session both stayed the same, so the page sees a consistent browser per visit and a new one on each launch. Brave's WebGL image was not changed (it matched Chrome's), and its font probe found only 1 of the 30 families, where the Chromium family found 6: Brave hides installed fonts from pages.
- **Camoufox changed its canvas on every launch, and usually its WebGL renderer too.** The renderer comes with the persona Camoufox picks from a limited set of common configurations, so in one of the five runs all three launches happened to get the same GPU. With a profile, the renderer and fonts came back the same (the stored persona), but the canvas still changed, as in [case 49](https://cdpfleet.com/docs/cases/camoufox-same-device).
- **Camoufox's rendered WebGL image and audio hash were identical to plain Firefox's on every launch**, whatever GPU the renderer string named. A script that compares the image with the claimed GPU can see that "NVIDIA" and "Apple M1" draw exactly like Firefox's software renderer. The audio hash is also stable, so it links Camoufox sessions to each other.
- **A profile saved by a newer browser build won't open on an older one.** While building this case the fleet was part-way through a Chrome update: a Chrome profile saved on a server with the new build was refused on one with the old build (`409 profile_version_downgrade`), and the next attempt landed on an updated server. The programs retry that error up to five times.
- For what each signal is and who reads it, see [What a page can read about your browser](https://cdpfleet.com/docs/fingerprinting); for headless and CDP tells, [case 16](https://cdpfleet.com/docs/cases/headless-tells) and [case 43](https://cdpfleet.com/docs/cases/cdp-detectability); for fonts, [case 52](https://cdpfleet.com/docs/cases/fonts-fingerprint); for what public bot-test pages conclude, [case 63](https://cdpfleet.com/docs/cases/what-fingerprint-test-pages-see). Measured on 2026-10-22; browser updates can change any of this.
