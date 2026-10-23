# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import json
import os
import secrets
import time
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
# Every setup gets its own sticky residential exit (letter = setup): DataDome scores the IP as well as the browser,
# so one setup's visits must not change what the next one sees.
NETWORKS = {
    "own": {"label": "own proxy (rotating)", "proxy": lambda exit_id: os.environ["PROXY_URL"]},
    "tier1": {"label": "cdpfleet-resi Tier 1, sticky US", "proxy": lambda exit_id: f"cdpfleet-resi-country-us-session-d{RUN}{exit_id}-lifetime-30"},
    "tier4": {"label": "cdpfleet-resi Tier 4, sticky US", "proxy": lambda exit_id: f"cdpfleet-resi-tier-4-country-us-session-d{RUN}{exit_id}-lifetime-30"},
}
SETUPS = {
    "chromium": {"id": "a", "setup": "Chromium, headless", "engine": "chromium", "type": "chromium", "headless": True},
    "chrome": {"id": "b", "setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "headless": False},
    "edge": {"id": "c", "setup": "Microsoft Edge, headful", "engine": "edge", "type": "chromium", "headless": False},
    "camoufox": {"id": "d", "setup": "Camoufox, defaults", "engine": "camoufox", "type": "firefox", "headless": True},
}
# DataDome's own public site, which DataDome protects: its home page and its pricing page.
PAGES = {
    "home": "https://datadome.co/",
    "pricing": "https://datadome.co/pricing/",
}

# What the page shows right now, in the lab's words. DataDome answers an intercepted request with a 403 page that
# embeds an iframe from captcha-delivery.com: a device check that passes by itself, a CAPTCHA, or a block (t=bv).
# The same expression runs in every language.
STATE = r"""(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()"""


# 503 means the fleet is momentarily busy: wait Retry-After and try again. A profile's second session can land on a
# node with an older build of the browser (409 profile_version_downgrade): ask again for another node.
def launch(p, s, network, exit_id=None, extra=None):
    body = {"proxy": NETWORKS[network]["proxy"](exit_id or s["id"]), "headless": s["headless"], "record": False, **(extra or {})}
    for attempt in range(1, 9):
        res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return getattr(p, s["type"]).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        retry = res.status_code in (429, 503) or (res.status_code == 409 and "profile_version_downgrade" in res.text)
        if not retry or attempt == 8:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


def goto(tab, url):
    response = tab.goto(url, wait_until="domcontentloaded", timeout=45000)
    return response.status if response else None


# Load a page (a network failure is retried once), then watch it once a second for 12 s without touching it.
# content = the page straight away; cleared = DataDome intercepted (403 or its device check) and let the browser
# through by itself; challenge = a CAPTCHA (never solved); blocked = DataDome's block page.
def visit(tab, url):
    t0 = time.time()
    try:
        try:
            status = goto(tab, url)
        except Exception:
            status = goto(tab, url)
    except Exception:
        return {"outcome": "error", "status": None, "seconds": None}
    checked = status == 403
    until = time.time() + 12  # the watch starts once the page has loaded, however slow the proxy was
    while time.time() < until:
        try:
            s = tab.evaluate(STATE)
        except Exception:
            s = None  # while the device check reloads the page
        if s in ("blocked", "challenge"):
            return {"outcome": s, "status": status, "seconds": None}
        if s == "checking":
            checked = True
        if s == "page":
            if checked:
                return {"outcome": "cleared", "status": status, "seconds": round(time.time() - t0)}
            return {"outcome": "content", "status": status, "seconds": None}
        time.sleep(1)
    return {"outcome": "challenge" if checked else "error", "status": status, "seconds": None}


# One session per setup and network; a fresh context per page so every load is a first visit.
def row(p, setup_key, network, pages):
    s = SETUPS[setup_key]
    browser = launch(p, s, network)
    out = {"setup": s["setup"], "network": NETWORKS[network]["label"]}
    try:
        for key in pages:
            context = browser.new_context()
            out[key] = visit(context.new_page(), PAGES[key])
            context.close()
    finally:
        browser.close()
    return out


def api(method, path):
    try:
        return requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=60).json()
    except Exception:
        return {}


def profile_info(name):
    return next((x for x in api("GET", "/v1/me/profiles").get("profiles", []) if x["name"] == name), None)


# A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart. The profile keeps
# the datadome cookie the first visit was given.
def returning(p, setup_key):
    s = SETUPS[setup_key]
    name = f"case65-{RUN}-{s['id']}"
    visits = []
    for _ in range(2):
        browser = launch(p, s, "tier1", exit_id=f"{s['id']}p", extra={"profile": name})
        try:
            context = browser.contexts[0]  # the profile's own context; a new_context() is not saved
            cell = visit(context.new_page(), PAGES["home"])
            # DataDome's script sets its cookie a moment after the page: give it up to 5 s.
            has_cookie = lambda: any(c["name"] == "datadome" for c in context.cookies())
            for _ in range(5):
                if has_cookie():
                    break
                time.sleep(1)
            cell["cookie"] = has_cookie()
            visits.append(cell)
        finally:
            browser.close()
        # The node saves the profile after the browser exits: wait until it is free again.
        for _ in range(60):
            info = profile_info(name)
            if info is not None and info.get("in_use") is False:
                break
            time.sleep(2)
    api("DELETE", f"/v1/me/profiles/{name}")
    return {"setup": s["setup"], "network": NETWORKS["tier1"]["label"], "first": visits[0], "second": visits[1]}


# Camoufox with humanize on and off: two page views in one context, with three mouse moves between them, so DataDome's
# script on the first page can see how the cursor moves before the second page is requested.
def humanize(p, on):
    browser = launch(p, SETUPS["camoufox"], "tier1", exit_id="h1" if on else "h0", extra={"humanize": on})
    try:
        tab = browser.new_context().new_page()
        first = visit(tab, PAGES["home"])
        t0 = time.time()
        for x, y in [(200, 300), (700, 250), (450, 600)]:
            tab.mouse.move(x, y)
        move_seconds = round(time.time() - t0, 1)
        second = visit(tab, PAGES["pricing"])
        return {"setup": f"Camoufox, humanize {'on' if on else 'off'}", "network": NETWORKS["tier1"]["label"], "home": first, "pricing": second, "move_seconds": move_seconds}
    finally:
        browser.close()


with sync_playwright() as p:
    matrix = [row(p, k, n, list(PAGES)) for n in ("own", "tier1") for k in ("chromium", "chrome", "edge", "camoufox")]
    tier4 = [row(p, k, "tier4", ["home"]) for k in ("chrome", "camoufox")]
    profiles = [returning(p, "chrome"), returning(p, "camoufox")]
    human = [humanize(p, True), humanize(p, False)]

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "matrix": matrix, "tier4": tier4, "profiles": profiles, "humanize": human}, indent=2))
