# Persistent profiles

Launch with `"profile": "shop-login"` and the browser starts from that profile: the cookies, localStorage and IndexedDB of the last session that used it. When the session ends, the profile is saved again. Log in once and stay logged in across sessions, machines and days, with no state files to keep yourself.

```
POST https://starter.cdpfleet.com/chrome/session
{ "proxy": "http://user:pass@proxy.example.com:8080", "profile": "shop-login" }
```

The first launch with a new name creates the profile; later launches restore it. Names are up to 64 letters, digits, `-` or `_`.

## Work in `browser.contexts()[0]`

That context **is** the profile. A context you create with `newContext()` (or `browser.newPage()`, which makes one) is a throwaway context and is **not saved**. This holds for Playwright `connect()` and `connectOverCDP` alike.

```js
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const context = browser.contexts()[0];
const page = context.pages()[0] ?? await context.newPage();
```

```python
browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
context = browser.contexts[0]
page = context.pages[0] if context.pages else context.new_page()
```

## What is saved

- **Saved:** cookies (session cookies included — captured before the browser closes and restored on the next launch, so `PHPSESSID`-style logins survive while the site keeps the session), localStorage, IndexedDB.
- **Not saved:** sessionStorage, which browsers keep per tab.
- **Not saved:** caches (HTTP, code, GPU and service-worker caches). A saved profile is typically a few kilobytes to a few megabytes.
- **When:** after the session ends and the browser has exited. If a server fails mid-session, that session's changes are lost and the previous version stays.
- **Camoufox keeps its device:** the fingerprint generated on the profile's first launch is saved and reused; later launches ignore fingerprint options (`os`, `screen_size`, `webgl_config`, `fonts`, `config` fingerprint keys). Timezone, locale and geolocation follow the exit IP, so keep a profile on one country or a sticky exit.

## Read-only and concurrency

- `"profile": { "id": "shop-login", "mode": "readonly" }` restores without saving; any number can run in parallel.
- A normal session holds the profile until it ends; a second normal launch gets `409 profile_in_use`.
- Works with `keep_alive` (saved when the session finally ends), live view and recordings.

## Browsers and limits

- Google Chrome, Microsoft Edge, Brave, Opera, Yandex Browser, Chromium, Patchright, Camoufox, Firefox and WebKit (`400 profile_unsupported_engine` on Naver Whale and Rebrowser).
- A profile belongs to the browser it was created with (`409 profile_engine_mismatch`) and can't be opened by an older major version (`409 profile_version_downgrade`).
- 20 profiles per shared thread and 30 per dedicated thread (at least 100 per account), 50 MB per saved profile, included in every paid plan.
- Over the limit (fewer threads, Community, or a plan that ended): nothing is deleted; new profiles can't be created, and 30 days later the ones beyond the limit are frozen (`403 profile_frozen`) — those marked `keep` (`PUT /v1/me/profiles/{name}` with `{"keep": true}`) and the most recently used stay active. Upgrading unfreezes them; profiles frozen for 90 days are deleted.
- Camoufox keeps its device (user agent, screen, fonts, WebGL) across launches; the canvas readback changes per browser session, as in stock Firefox. List and delete with `GET https://cdpfleet.com/v1/me/profiles` and `DELETE https://cdpfleet.com/v1/me/profiles/{name}`.

Profiles are encrypted with a key of their own and stored privately in Germany; browser servers never hold storage credentials.

Full guide: [cdpfleet.com/docs/profiles](https://cdpfleet.com/docs/profiles).
