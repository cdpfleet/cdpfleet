# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import json
import os
import re
import secrets
import time
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
# The sticky residential exit is shared by every setup, so a difference between rows is the browser.
NETWORKS = {
    "own": {"label": "own proxy (rotating)", "proxy": os.environ["PROXY_URL"]},
    "tier1": {"label": "cdpfleet-resi Tier 1, sticky US", "proxy": f"cdpfleet-resi-country-us-session-c68{RUN}-lifetime-30"},
}
SETUPS = {
    "chromium": {"setup": "Chromium, headless", "engine": "chromium", "type": "chromium", "headless": True},
    "chrome": {"setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "headless": False},
    "edge": {"setup": "Microsoft Edge, headful", "engine": "edge", "type": "chromium", "headless": False},
    "brave": {"setup": "Brave, headful", "engine": "brave", "type": "chromium", "headless": False},
    "camoufox": {"setup": "Camoufox, defaults", "engine": "camoufox", "type": "firefox", "headless": True},
    "firefox": {"setup": "Firefox, headful", "engine": "firefox", "type": "firefox", "headless": False},
    "webkit": {"setup": "WebKit, headful", "engine": "webkit", "type": "webkit", "headless": False},
}
# Vendors' own public demo pages. Google's v3 demo asks for a score as soon as it loads and prints its
# server's verify response (with the score) on the page; hCaptcha's demo shows a checkbox widget.
RECAPTCHA = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php"
HCAPTCHA = "https://accounts.hcaptcha.com/demo"

# Read what the page shows; the same expressions run in every language.
SCORE = r"""(() => { const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()"""
HC_WIDGET = """(() => { const f = document.querySelector('iframe[src*="hcaptcha.com"]'); return f ? (f.getBoundingClientRect().height > 0) : false; })()"""


def launch(p, s, network):
    body = {"proxy": NETWORKS[network]["proxy"], "headless": s["headless"], "record": False}
    # WebKit takes an authenticated HTTP proxy through the updatable route (see the WebKit page).
    if s["engine"] == "webkit" and re.match(r"^https?:", body["proxy"]):
        body["proxy_updatable"] = True
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return getattr(p, s["type"]).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


def safe(tab, expr, default):
    try:
        return tab.evaluate(expr)
    except Exception:
        return default


# One load; then wait up to 20 s for the score the demo prints. Nothing on the page is touched.
def recaptcha(context):
    tab = context.new_page()
    try:
        tab.goto(RECAPTCHA, wait_until="domcontentloaded", timeout=45000)
    except Exception:
        tab.close()
        return {"outcome": "error", "score": None}
    score = None
    for _ in range(20):
        time.sleep(1)
        score = safe(tab, SCORE, None)
        if score is not None:
            break
    tab.close()
    return {"outcome": "no score" if score is None else "score", "score": score}


# hCaptcha only decides when someone clicks its checkbox, so a load shows whether the widget renders, nothing more.
def hcaptcha(context):
    tab = context.new_page()
    try:
        tab.goto(HCAPTCHA, wait_until="domcontentloaded", timeout=45000)
    except Exception:
        tab.close()
        return {"outcome": "error"}
    shown = False
    for _ in range(12):
        time.sleep(1)
        shown = safe(tab, HC_WIDGET, False)
        if shown:
            break
    tab.close()
    return {"outcome": "widget shown" if shown else "no widget"}


def row(p, setup_key, network):
    s = SETUPS[setup_key]
    out = {"setup": s["setup"], "network": NETWORKS[network]["label"]}
    try:
        browser = launch(p, s, network)
    except Exception:
        return {**out, "recaptcha": {"outcome": "error", "score": None}, "hcaptcha": {"outcome": "error"}}
    # A session the server ends (e.g. its memory limit) leaves the remaining cells as errors.
    for key, fn in (("recaptcha", recaptcha), ("hcaptcha", hcaptcha)):
        try:
            context = browser.new_context()  # a fresh context: every load is a first visit
            out[key] = fn(context)
            context.close()
        except Exception:
            out[key] = {"outcome": "error", "score": None} if key == "recaptcha" else {"outcome": "error"}
    try:
        browser.close()
    except Exception:
        pass
    return out


with sync_playwright() as p:
    rows = [row(p, setup_key, network) for network in ("own", "tier1") for setup_key in SETUPS]

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "rows": rows}, indent=2))
