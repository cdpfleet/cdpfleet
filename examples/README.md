# Examples

Complete programs in Node.js, Python, Java, C# and Go. Each one launches a browser over the cdpfleet API, connects with Playwright and closes the session.

- [quickstart/](quickstart) — the shortest path to a working session
- browsers/ — one minimal program per browser:
  - [Google Chrome](browsers/chrome)
  - [Microsoft Edge](browsers/edge)
  - [Brave](browsers/brave)
  - [Opera](browsers/opera)
  - [Naver Whale](browsers/whale)
  - [Yandex Browser](browsers/yandex)
  - [Chromium](browsers/chromium)
  - [Patchright](browsers/patchright)
  - [Rebrowser](browsers/rebrowser)
  - [CloakBrowser](browsers/cloakbrowser)
  - [Camoufox](browsers/camoufox)
  - [Firefox](browsers/firefox)
  - [WebKit](browsers/webkit)
- recipes/ — common setups:
  - [Google Chrome, beta channel](recipes/chrome-beta)
  - [Camoufox with a Windows fingerprint](recipes/camoufox-fingerprint)
  - [Proxy pool with per-host rules](recipes/proxy-pool-and-rules)
  - [SOCKS5 proxy with authentication](recipes/socks5-proxy)
  - [Patchright, headful, for stricter sites](recipes/headful-stealth)
  - [Camoufox: pinned version, macOS persona, no images](recipes/camoufox-lean-persona)
  - [Short-lived sessions for burst jobs](recipes/burst-timeouts)
  - [Camoufox with a fixed screen and window](recipes/camoufox-fixed-screen)
  - [Rebrowser with the Playwright 1.52 client](recipes/rebrowser)
  - [Microsoft Edge, beta channel](recipes/edge-beta)
  - [Brave with Shields on](recipes/brave)
  - [Camoufox as a Windows laptop with Intel graphics](recipes/camoufox-intel-laptop)
  - [Chrome over CDP (connectOverCDP or Puppeteer)](recipes/chrome-cdp)
  - [Live view: stream the page to your code](recipes/live-view)
  - [cdpfleet residential: one sticky US identity](recipes/resi-sticky)
  - [Camoufox with exact fingerprint values](recipes/camoufox-config)
  - [WebKit (Safari's engine)](recipes/webkit)
  - [Opera, developer channel](recipes/opera-developer)
  - [Naver Whale](recipes/whale)
  - [CloakBrowser, headful, full-HD display](recipes/cloakbrowser-stealth)
  - [Yandex Browser](recipes/yandex)
  - [Camoufox reporting an older Firefox](recipes/camoufox-ff-version)
  - [Camoufox with a macOS font list](recipes/camoufox-mac-fonts)
  - [Long-running session](recipes/long-session)

Every example reads your key from `CDPFLEET_API_KEY` and uses a placeholder proxy — replace it with yours. For longer, real-world programs (fingerprints, proxy swaps, worker pools, files, WebSockets…) see [cases/](../cases).

Generated from the same code generator as the [code builder](https://cdpfleet.com/docs/builder).
