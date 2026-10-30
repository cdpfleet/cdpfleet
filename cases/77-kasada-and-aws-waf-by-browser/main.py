# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, LAB_SITES_KASADA_AWSWAF (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import asyncio
import json
import os
import re
import secrets
import time
from datetime import datetime, timezone

import requests
from playwright.async_api import async_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
SITES = [{"site": chr(65 + i), "url": u} for i, u in enumerate(os.environ["LAB_SITES_KASADA_AWSWAF"].split(","))]
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

# What the page shows after it loaded and had 8 s to run its checks, in the lab's words. AWS WAF's
# CAPTCHA ("Let's confirm you are human") is interactive: a challenge. A near-empty page 8 s later
# (Kasada's 429 answer is a blank page with a script) or an "access denied" page is blocked.
STATE = r"""(() => {
  const text = document.body ? document.body.innerText : '';
  const t = (document.title + ' ' + text.slice(0, 600)).replace(/\s+/g, ' ');
  if (document.querySelector('#captcha-container, awswaf-captcha') || (/confirm you are human|verify you are (a )?human|are you a robot|solve the puzzle/i.test(t) && text.length < 4000)) return 'challenge';
  if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request (unsuccessful|blocked)|too many requests/i.test(t) && text.length < 4000) return 'blocked';
  if (text.trim().length < 200) return 'blank';
  return 'content';
})()"""

KASADA_URL = re.compile(r"/ips\.js(\?|$)|/(tl|fp)(\?|$)")
AWS_URL = re.compile(r"awswaf\.com|/challenge\.js(\?|$)")
NONE = {"outcome": "error", "status": 0, "final_status": 0, "kasada": False, "aws_waf": False, "cookies": []}


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


# One session, one load. Records the status of the page's first response and of the last main-frame
# document 8 s later (an interstitial that clears itself reloads the page), which vendor's scripts,
# headers or cookies showed up (cookie and header NAMES only; values are never read), and the state.
async def visit(pw, setup, url, proxy, profile=None):
    _, _, engine, kind, headless = setup
    browser = await getattr(pw, kind).connect(await launch(engine, headless, proxy, profile), headers={"x-api-key": KEY})
    try:
        context = browser.contexts[0] if profile else await browser.new_context()
        tab = await context.new_page()
        seen = {"kasada": False, "aws_waf": False, "waf_action": None, "docs": []}
        t0 = time.monotonic()

        def on_request(r):
            if KASADA_URL.search(r.url) or any(h.startswith("x-kpsdk") for h in r.headers):
                seen["kasada"] = True
            if AWS_URL.search(r.url):
                seen["aws_waf"] = True

        def on_response(r):
            h = r.headers
            if any(n.startswith("x-kpsdk") for n in h):
                seen["kasada"] = True
            action = h.get("x-amzn-waf-action")
            if action:
                seen["aws_waf"] = True
                if action in ("challenge", "captcha") and not seen["waf_action"]:
                    seen["waf_action"] = action
            if r.request.is_navigation_request() and r.frame == tab.main_frame:
                seen["docs"].append((r.status, time.monotonic() - t0))

        tab.on("request", on_request)
        tab.on("response", on_response)
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
        final_status, final_at = seen["docs"][-1] if seen["docs"] else (status, 0)
        cleared_s = None
        # A 202/429 check that passed by itself reloads the page with a success status: cleared, even if
        # the new page hasn't rendered yet at the 8 s mark. Without that reload it never cleared.
        if outcome in ("content", "blank"):
            if final_status >= 400:
                outcome = "blocked"
            elif status >= 400 or status == 202:
                if len(seen["docs"]) > 1 and final_status != 202:
                    outcome, cleared_s = "cleared", round(final_at, 1)
                else:
                    outcome = "blocked"
            elif outcome == "blank":
                outcome = "blocked"
        cookies = sorted({c["name"] for c in await context.cookies() if re.match(r"(?i)^(KP_|aws-waf)", c["name"])})
        if any(n.upper().startswith("KP_") for n in cookies):
            seen["kasada"] = True
        if any(n.lower().startswith("aws-waf") for n in cookies):
            seen["aws_waf"] = True
        out = {"outcome": outcome, "status": status, "final_status": final_status}
        if cleared_s is not None:
            out["cleared_s"] = cleared_s
        out.update({"kasada": seen["kasada"], "aws_waf": seen["aws_waf"]})
        if seen["waf_action"]:
            out["waf_action"] = seen["waf_action"]
        out["cookies"] = cookies
        return out
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
    proxy = f"cdpfleet-resi-country-us-session-c77{RUN}{s['site']}-lifetime-30"
    results = {}
    for setup in SETUPS:
        r = await safe(pw, setup, s["url"], proxy)
        if r["outcome"] == "error":
            r = await safe(pw, setup, s["url"], proxy)
        results[setup[0]] = r
    # A returning visitor: Camoufox with a profile, a first visit and then a second session.
    name = f"case77-{RUN}-{s['site']}"
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
