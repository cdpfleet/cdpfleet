# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, LAB_SITES_HUMAN (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import asyncio
import json
import os
import secrets
from datetime import datetime, timezone

import requests
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
SITES = [{"site": chr(65 + i), "url": u} for i, u in enumerate(os.environ["LAB_SITES_HUMAN"].split(","))]
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

# What the page shows after it loaded and had 8 s to run its checks, in the lab's words. HUMAN's
# challenge is its "Press & Hold" widget (#px-captcha), usually on a page titled "Access to this page
# has been denied" — interactive, so it counts as a challenge, not a block.
STATE = r"""(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\s+/g, ' ');
  const len = document.body ? document.body.innerText.length : 0;
  if (document.querySelector('#px-captcha') || (/press (&|and) hold|human touch|verify you are (a )?human|are you a robot|before we continue/i.test(t) && len < 4000)) return 'challenge';
  if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request unsuccessful|reference #/i.test(t) && len < 4000) return 'blocked';
  return 'content';
})()"""

NONE = {"outcome": "error", "status": 0, "px_cookies": []}


# HUMAN's cookies (_px3, _pxhd, _pxvid, pxcts …): which ones the site set, by name only — values are never read.
def px_cookies(cookies):
    return sorted(c["name"] for c in cookies if c["name"].lower().lstrip("_").startswith("px"))


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


# One session, one load: the status of the page's own response, then the page's state and HUMAN's
# cookies 8 s later.
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
            # A reset HTTP/2 connection is a refusal, not a network failure.
            return {**NONE, "outcome": "blocked" if "ERR_HTTP2_PROTOCOL_ERROR" in str(err) else "error"}
        await tab.wait_for_timeout(8000)
        try:
            outcome = await tab.evaluate(STATE)
        except Exception:
            outcome = "error"
        if outcome == "content" and status >= 400:
            outcome = "blocked"
        return {"outcome": outcome, "status": status, "px_cookies": px_cookies(await context.cookies())}
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
    proxy = f"cdpfleet-resi-country-us-session-c75{RUN}{s['site']}-lifetime-30"
    results = {}
    for setup in SETUPS:
        r = await safe(pw, setup, s["url"], proxy)
        if r["outcome"] == "error":
            r = await safe(pw, setup, s["url"], proxy)
        results[setup[0]] = r
    # A returning visitor: Camoufox with a profile, a first visit and then a second session.
    name = f"case75-{RUN}-{s['site']}"
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
