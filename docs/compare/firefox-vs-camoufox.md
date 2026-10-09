# Firefox vs Camoufox on cdpfleet

Firefox and Camoufox share the same engine family — Camoufox is a fork of Firefox — and both use the same Playwright client (`firefox 1.60`). But they serve very different purposes. Firefox gives you a clean, unmodified browser. Camoufox wraps that engine with engine-level fingerprint spoofing, geo-matching, and humanized input to make automated sessions look like real users. This page helps you decide which one fits your use case.

## Quick comparison

| Feature | Firefox | Camoufox |
|---|---|---|
| **Engine** | Playwright's Firefox build | Firefox fork with fingerprint spoofing |
| **Endpoint** | `POST /firefox/session` | `POST /camoufox/session` (also `POST /session`) |
| **Playwright client** | `firefox 1.60` | `firefox 1.60` |
| **Configurable options** | ~8 | 20+ |
| **Fingerprint spoofing** | None | Navigator, screen, WebGL, canvas, timezone, locale, fonts |
| **OS persona** | Reports real server OS | Windows, macOS, or Linux — your choice |
| **Geo-matching** | None | Timezone, locale, geolocation, WebRTC matched to proxy IP |
| **Humanized cursor** | No | Yes, enabled by default |
| **WebGL spoofing** | No | Yes — custom vendor/renderer pairs |
| **Canvas spoofing** | No | Yes |
| **Version pinning** | No | Yes — pin older major versions |
| **Launch overhead** | Faster | Slightly heavier (fingerprint generation) |
| **Best for** | Testing, unprotected scraping | Anti-detect scraping, protected sites |

## Engine and client

Both browsers connect through the same Playwright client (`firefox 1.60`) and expose the same Playwright API. Code written for one works with the other — the difference is in what the browser reports about itself to the pages it visits, not in how you control it.

Firefox uses Playwright's standard Firefox build. Camoufox uses a modified Firefox build that intercepts and spoofs the values bot-detection scripts check.

## Detection profile

**Firefox** reports exactly what the server environment provides. A bot-detection script checking `navigator.webdriver`, WebGL renderer strings, screen dimensions, timezone, or installed fonts will see values consistent with a Linux server running in a data center. This is fine for sites that do not run detection — and many do not. But any site using a fingerprinting service will immediately flag the session as automated.

**Camoufox** generates a complete, internally consistent fingerprint. When you set `os: "windows"`, the browser reports Windows-appropriate values for `navigator.platform`, `navigator.userAgent`, screen dimensions, available fonts, WebGL renderer, and more. The timezone and locale match your proxy's exit IP by default. Canvas and WebGL outputs are spoofed at the engine level, not through JavaScript hooks that detection scripts can spot.

Key differences in what detection scripts see:

| Signal | Firefox | Camoufox |
|---|---|---|
| `navigator.webdriver` | May be detectable | Spoofed |
| `navigator.platform` | Server's real OS | Matches chosen OS persona |
| Screen dimensions | Server display or headless defaults | Realistic values for chosen OS |
| WebGL vendor/renderer | Server GPU or software renderer | Configurable realistic pair |
| Canvas fingerprint | Real server output | Spoofed at engine level |
| Timezone | Server timezone | Matched to proxy IP |
| Installed fonts | Server fonts | OS-appropriate font list |
| Cursor movement | Instant, mechanical | Human-like trajectories |

## Configuration

**Firefox options** — straightforward session control:

- `proxy` (required)
- `proxy_rules`
- `inactivity_timeout`
- `overall_timeout`
- `headless`
- `screencast`
- `screen_size`
- `keep_alive`
- `record`

**Camoufox options** — everything Firefox has, plus identity and fingerprint controls:

- All Firefox options
- `os` — Windows, macOS, or Linux persona
- `locale` — browser locale(s), derived from proxy IP by default
- `geoip` — match timezone/locale/geolocation/WebRTC to proxy exit IP (default: true)
- `humanize` — human-like cursor movement (default: true)
- `block_images`, `block_webrtc`, `block_webgl`
- `disable_coop`
- `screen` — constrain the fingerprint's screen size
- `window` — fixed outer window size
- `fingerprint` — supply a complete BrowserForge fingerprint
- `config` — override individual fingerprint properties (`navigator.hardwareConcurrency`, `navigator.maxTouchPoints`, `screen.width`, `screen.height`, etc.)
- `fonts`, `custom_fonts_only`
- `webgl_config` — force a specific WebGL vendor/renderer pair
- `ff_version` — Firefox major version to claim
- `main_world_eval` — allow scripts in main world
- `version` — pin an older Camoufox major version

## User experience

**Firefox** is the simpler choice. You set your proxy, optionally configure timeouts and screen size, and launch. Sessions start faster because there is no fingerprint generation step. Configuration is minimal.

**Camoufox** requires a moment more thought. You choose an OS persona, decide whether to rely on auto-generated fingerprints or supply your own, and optionally fine-tune WebGL, fonts, and other identity signals. Sessions take slightly longer to start due to fingerprint generation. The payoff is a browser that looks like a real user on a real machine.

For most Camoufox use cases, the defaults are good enough — `os`, `proxy`, and the automatic geo-matching handle the common case without manual fingerprint tuning.

## Scraper experience

**Firefox** works well for scraping sites that do not deploy bot detection. News sites, public data portals, government databases, documentation sites, and many e-commerce sites with light or no fingerprinting will work fine with plain Firefox. It is also the right choice for functional testing of Firefox-specific behavior.

**Camoufox** is built for sites that actively fight automation. If the target site uses Cloudflare Bot Management, DataDome, PerimeterX, Akamai Bot Manager, or similar services, Camoufox gives you a much better chance of accessing the content without triggering challenges or blocks.

## What you gain with Camoufox

- **Fingerprint spoofing** at the engine level — not JavaScript injection that detection scripts can catch.
- **OS persona** — appear as a Windows, macOS, or Linux user regardless of server OS.
- **Geo-matching** — timezone, locale, geolocation, and WebRTC automatically matched to your proxy's exit IP.
- **Humanized cursor** — mouse movements follow natural trajectories instead of teleporting between elements.
- **Custom WebGL** — set specific vendor/renderer pairs to match your persona.
- **Version pinning** — pin an older major version if a target site flags the latest.
- **Full fingerprint control** — supply a BrowserForge fingerprint or override individual properties.

## What you lose with Camoufox

Nothing in API capability. Camoufox is a strict superset of Firefox's options. Every Playwright method that works with Firefox works with Camoufox. The only costs are:

- Slightly longer session startup (fingerprint generation).
- More configuration options to understand (though defaults are sensible).
- A modified engine rather than the stock Playwright Firefox build, if you need exact parity with upstream Firefox behavior for testing purposes.

## When to choose each

**Choose Firefox when:**

- You are testing Firefox-specific rendering or behavior.
- The target site has no bot detection or light fingerprinting.
- You want the fastest possible session startup.
- You need an unmodified browser engine for compatibility testing.
- Simplicity matters more than stealth.

**Choose Camoufox when:**

- The target site uses bot-detection or fingerprinting services.
- You need to appear as a real user on a specific OS.
- Your proxy identity should be matched with consistent timezone, locale, and geolocation.
- You want human-like cursor movement out of the box.
- You need to control WebGL, canvas, fonts, or other fingerprint signals.
- You are building scrapers that must survive bot-detection updates.

If you are unsure, start with Firefox. If you hit blocks or CAPTCHAs, switch to Camoufox — the Playwright API is the same, so the change is just a different endpoint and a few extra options in your session request.

## Launch examples

**Firefox** — minimal session request:

```json
{
  "proxy": "http://user:pass@proxy.example.com:8080"
}
```

Endpoint: `POST https://starter.cdpfleet.com/firefox/session`

**Camoufox** — session with OS persona and geo-matching:

```json
{
  "proxy": "http://user:pass@proxy.example.com:8080",
  "os": "windows",
  "geoip": true,
  "humanize": true
}
```

Endpoint: `POST https://starter.cdpfleet.com/camoufox/session`

**Camoufox** — session with full fingerprint control:

```json
{
  "proxy": "http://user:pass@proxy.example.com:8080",
  "os": "macos",
  "webgl_config": {
    "vendor": "Apple",
    "renderer": "Apple M1"
  },
  "config": {
    "navigator.hardwareConcurrency": 8,
    "screen.width": 2560,
    "screen.height": 1440
  },
  "ff_version": 131,
  "humanize": true
}
```

Endpoint: `POST https://starter.cdpfleet.com/camoufox/session`
