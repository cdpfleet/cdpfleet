# cdpfleet vs Browserbase: cloud browser APIs compared

cdpfleet and Browserbase both provide cloud-hosted browsers accessible via API, but they take fundamentally different approaches. Browserbase offers a managed Chromium environment with built-in proxies, CAPTCHA solving, and fingerprint management. cdpfleet gives you 13 real browsers — including purpose-built anti-detect engines — with a bring-your-own-proxy model that keeps your IP identity under your control. This page compares the two services across the dimensions that matter most for browser automation.

## Quick comparison

| Feature | cdpfleet | Browserbase |
|---|---|---|
| **Browsers available** | 13 (Chrome, Edge, Brave, Opera, Whale, Yandex, Chromium, Patchright, Rebrowser, CloakBrowser, Camoufox, Firefox, WebKit) | Chromium-based |
| **Anti-detect browsers** | Camoufox, Patchright, CloakBrowser, Rebrowser | Built-in fingerprint management |
| **Proxy model** | Bring your own (residential option built-in) | Built-in proxy rotation |
| **CAPTCHA solving** | Not included | Built-in |
| **Language support** | Node.js, Python, Java, C#, Go (all via Playwright) | Playwright, Puppeteer, Selenium |
| **CDP access** | Yes, alongside Playwright on compatible browsers | Yes |
| **Session recording** | 480p free, 720p/1080p on plans | Available |
| **Live view** | Yes — watch and control headless sessions | Yes |
| **Keep-alive / reconnect** | Yes | Yes |
| **Version pinning** | Multiple release channels (stable, beta, dev) + version pinning for Chrome and Camoufox | Limited |
| **Pricing** | Usage-based — see [cdpfleet.com/pricing](https://cdpfleet.com/pricing) | Free tier, then $20-99+/mo with per-hour rates ($0.10-0.12/hr) |

## Browser choice

cdpfleet runs 13 distinct browsers. This is not 13 skins on Chromium — it includes Firefox, WebKit, and multiple Chromium-based browsers each with their own engine builds and user-agent signatures. You can test across Chrome, Edge, Brave, Opera, and others, or use anti-detect variants like Camoufox (a Firefox fork) for fingerprint diversity that Chromium alone cannot provide.

Browserbase focuses on a Chromium-based browser. For teams whose workload is entirely Chromium, this is simpler. For teams that need to test across engines or want browser diversity to avoid detection, cdpfleet covers more ground.

## Anti-detection

cdpfleet ships four purpose-built anti-detect browsers:

- **Camoufox** — a Firefox fork with engine-level fingerprint spoofing (navigator, screen, WebGL, canvas, timezone, locale, fonts), OS persona selection, geo-matching to proxy IP, and humanized cursor movement.
- **Patchright** — a stealth Chromium build designed to pass bot-detection checks.
- **CloakBrowser** — a fingerprint-spoofing Chromium variant.
- **Rebrowser** — a stealth Chromium build (v1.52) focused on detection evasion.

Because these are separate browser builds, the spoofing happens at the engine level rather than through JavaScript injection, making it harder for detection scripts to spot inconsistencies.

Browserbase handles anti-detection through its built-in fingerprint management system applied to its Chromium browser. This is a valid approach, and for many use cases it works well. The difference is that cdpfleet gives you multiple independent browser engines to choose from, each with its own detection profile.

## Proxy model

cdpfleet requires you to bring your own proxy. Every session runs through your proxy, so the IP address and geographic identity are yours to control. cdpfleet also offers a built-in residential proxy option. You can configure per-destination proxy rules — routing different hosts through different proxies in a single session.

Browserbase includes built-in proxy rotation as part of the service. This is more convenient if you do not already have proxy infrastructure. The tradeoff is less control over which IPs are used and shared across customers.

Neither approach is inherently better — it depends on whether you want managed convenience or full control over your network identity.

## Language support

cdpfleet supports five languages — Node.js, Python, Java, C#, and Go — all through Playwright's client libraries. The connection model is the same across all languages: POST to the session endpoint, receive a WebSocket URL, connect via Playwright.

Browserbase supports Playwright, Puppeteer, and Selenium as client frameworks. This gives more flexibility in JavaScript tooling (Puppeteer and Playwright), plus Selenium for teams already using it.

## Pricing model

cdpfleet uses usage-based pricing. See [cdpfleet.com/pricing](https://cdpfleet.com/pricing) for current rates. Session recording at 480p is included free; 720p and 1080p recording is available on paid plans.

Browserbase offers a free tier, a Developer plan at $20/month ($0.12/hr), a Startup plan at $99/month ($0.10/hr), and custom Scale pricing. CAPTCHA solving and proxy rotation are included in these plans.

## Session management

Both services provide session recording, live view, and keep-alive capabilities.

cdpfleet sessions are headful by default, running on a virtual display. You can switch to headless mode per session. Screen size is configurable. The live view lets you watch and interact with headless sessions in real time. Keep-alive allows you to disconnect from a session and reconnect later without losing browser state.

Browserbase provides similar session management features, plus a Search API for extracting structured data and a Functions API for defining reusable browser actions.

## Developer experience

cdpfleet is Playwright-native. You create a session with a single POST request specifying the browser and options, then connect using Playwright's standard `connect` method with the returned WebSocket URL. CDP (Chrome DevTools Protocol) access is available alongside Playwright on compatible browsers.

```
POST https://starter.cdpfleet.com/{browser}/session
```

Browserbase provides SDKs and supports multiple automation frameworks. It also offers an Identity module for managing authentication state across sessions and a Model gateway for AI agent integration.

## When to choose cdpfleet

- You need browsers beyond Chromium — Firefox, WebKit, or specific Chromium variants.
- You want engine-level anti-detection through dedicated browser builds like Camoufox.
- You need to control your own proxy infrastructure and IP identity.
- You want to pin specific browser versions or use beta/dev release channels.
- Your stack uses Java, C#, or Go alongside Python and Node.js.
- You need per-destination proxy routing within a single session.

## When to choose Browserbase

- You want a fully managed solution with built-in proxies and CAPTCHA solving.
- Your workload is entirely Chromium-based and you do not need browser diversity.
- You prefer Puppeteer or Selenium over Playwright.
- You want integrated AI model gateway features.
- You prefer bundled pricing with proxies and CAPTCHA solving included.

## Summary

cdpfleet and Browserbase serve overlapping but distinct needs. cdpfleet offers broader browser coverage, dedicated anti-detect engine builds, and a bring-your-own-proxy model that gives you full control over your network identity. Browserbase offers a more managed experience with built-in proxies, CAPTCHA solving, and support for multiple automation frameworks. The right choice depends on whether you prioritize browser diversity and proxy control or managed convenience and bundled services.
