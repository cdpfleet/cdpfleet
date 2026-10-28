# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, LAB_SITES_AKAMAI (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import asyncio
import json
import os
import secrets
from datetime import datetime, timezone

import requests
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
SITES = [{"site": chr(65 + i), "url": u} for i, u in enumerate(os.environ["LAB_SITES_AKAMAI"].split(","))]
# Every browser the fleet offers that a Playwright 1.60 client can drive (Rebrowser needs 1.52, so it's left out).
SETUPS = [
    ("chrome_headful", "Chrome, headful", "chrome", "chromium", False),
    ("chrome_headless", "Chrome, headless", "chrome", "chromium", "new"),
    ("edge", "Edge, headful", "edge", "chromium", False),
    ("brave", "Brave, headful", "brave", "chromium", False),
    ("opera", "Opera, headful", "opera", "chromium", False),
    ("yandex", "Yandex, headful", "yandex", "chromium", False),
    ("whale", "Whale, headful", "whale", "chromium", False),
    ("chromium_headless", "Chromium, headless", "chromium", "chromium", True),
    ("patchright", "Patchright, headful", "patchright", "chromium", False),
    ("firefox", "Firefox, headful", "firefox", "firefox", False),
    ("webkit", "WebKit, headful", "webkit", "webkit", False),
    ("camoufox", "Camoufox, headful", "camoufox", "firefox", False),
]

# What the page shows after it loaded and had 8 s to run its checks, in the lab's words.
STATE = r"""(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\s+/g, ' ');
  const len = document.body ? document.body.innerText.length : 0;
  if (/access denied|you don.t have permission to access|pardon our interruption|request unsuccessful|reference #/i.test(t) && len < 4000) return 'blocked';
  if (/verify you are (a )?human|press (&|and) hold|are you a robot|just a moment|checking your browser/i.test(t) && len < 4000) return 'challenge';
  return 'content';
})()"""

NONE = {"outcome": "error", "status": 0, "abck_before": None, "abck_after": None}


# Akamai's _abck cookie: the second ~-separated field is its status (-1 while the sensor data
# hasn't validated the visitor, 0 once it has). Read only, never written.
def abck_status(cookies):
    c = next((x for x in cookies if x["name"] == "_abck"), None)
    if not c:
        return None
    parts = c["value"].split("~")
    return parts[1] if len(parts) > 1 else "?"


def api(method, path):
    try:
        return requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=30).json()
    except Exception:
        return {}


async def launch(engine, headless, proxy, profile=None):
    body = {"proxy": proxy, "headless": headless, "record": False, **({"profile": profile} if profile else {})}
    for attempt in range(1, 6):
        res = await asyncio.to_thread(requests.post, f"https://starter.cdpfleet.com/{engine}/session",
                                      headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return res.json()["wsUrl"]
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        await asyncio.sleep(int(res.headers.get("retry-after") or 5))


# One session, one load: the status of the page's own response, _abck right after it, then the
# page's state and _abck 8 s later.
async def visit(pw, setup, url, proxy, profile=None):
    _, _, engine, kind, headless = setup
    browser = await getattr(pw, kind).connect(await launch(engine, headless, proxy, profile), headers={"x-api-key": KEY})
    try:
        context = browser.contexts[0] if profile else await browser.new_context()
        tab = await context.new_page()
        try:
            res = await tab.goto(url, wait_until="domcontentloaded", timeout=45000)
            status = res.status if res else 0
        except Exception as err:
            # Akamai refuses some clients by resetting the HTTP/2 connection: a block, not a network failure.
            return {**NONE, "outcome": "blocked" if "ERR_HTTP2_PROTOCOL_ERROR" in str(err) else "error"}
        abck_before = abck_status(await context.cookies())
        await tab.wait_for_timeout(8000)
        try:
            outcome = await tab.evaluate(STATE)
        except Exception:
            outcome = "error"
        if outcome == "content" and status >= 400:
            outcome = "blocked"
        return {"outcome": outcome, "status": status, "abck_before": abck_before, "abck_after": abck_status(await context.cookies())}
    finally:
        await browser.close()


async def safe(pw, setup, url, proxy, profile=None):
    try:
        return await visit(pw, setup, url, proxy, profile)
    except Exception:
        return dict(NONE)


# One sticky residential exit per site; its setups run one after another (one session at a time per
# site), and the sites run side by side. A network error gets one more try.
async def site(pw, s):
    proxy = f"cdpfleet-resi-country-us-session-c73{RUN}{s['site']}-lifetime-30"
    results = {}
    for setup in SETUPS:
        r = await safe(pw, setup, s["url"], proxy)
        if r["outcome"] == "error":
            r = await safe(pw, setup, s["url"], proxy)
        results[setup[0]] = r
    # A returning visitor: Camoufox with a profile, a first visit and then a second session.
    name = f"case73-{RUN}-{s['site']}"
    camoufox = next(x for x in SETUPS if x[0] == "camoufox")
    await safe(pw, camoufox, s["url"], proxy, name)
    for _ in range(60):
        p = next((p for p in api("GET", "/v1/me/profiles").get("profiles", []) if p["name"] == name), None)
        if p and p.get("in_use") is False:
            break
        await asyncio.sleep(2)
    results["camoufox_returning"] = await safe(pw, camoufox, s["url"], proxy, name)
    api("DELETE", f"/v1/me/profiles/{name}")
    return {"site": s["site"], "results": results}


async def main():
    async with async_playwright() as pw:
        sites = await asyncio.gather(*(site(pw, s) for s in SITES))
    setups = {x[0]: x[1] for x in SETUPS}
    setups["camoufox_returning"] = "Camoufox, returning profile (2nd visit)"
    print(json.dumps({"measured_at": datetime.now(timezone.utc).date().isoformat(), "setups": setups, "sites": sites}, indent=2))


asyncio.run(main())
