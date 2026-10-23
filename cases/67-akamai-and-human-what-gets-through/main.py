# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import json
import os
import re
import secrets
import time
from datetime import datetime, timezone
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
# Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
NETWORKS = {
    "own": {"label": "own proxy (rotating)", "proxy": os.environ["PROXY_URL"]},
    "tier1": {"label": "cdpfleet-resi Tier 1, sticky US", "proxy": f"cdpfleet-resi-country-us-session-c67{RUN}-lifetime-30"},
    "tier4": {"label": "cdpfleet-resi Tier 4, sticky US", "proxy": f"cdpfleet-resi-tier-4-country-us-session-c67{RUN}-lifetime-30"},
}
SETUPS = {
    "chromium": {"setup": "Chromium, headless", "engine": "chromium", "type": "chromium", "headless": True},
    "chrome": {"setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "headless": False},
    "edge": {"setup": "Microsoft Edge, headful", "engine": "edge", "type": "chromium", "headless": False},
    "camoufox": {"setup": "Camoufox, defaults", "engine": "camoufox", "type": "firefox", "headless": True},
}
# Each vendor's own home page, which that vendor protects with its own product.
PAGES = {
    "akamai": {"url": "https://www.akamai.com/", "sensor": lambda u: urlparse(u).hostname == "www.akamai.com"},
    "human": {"url": "https://www.humansecurity.com/", "sensor": lambda u: bool(re.search(r"(^|\.)px-(cloud|cdn)\.net$", urlparse(u).hostname or ""))},
}

# What the page shows right now, in the lab's words. The same expression runs in every language.
STATE = r"""(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const blocked = /access denied|you don't have permission to access|request blocked|reference #\d/i.test(t);
  const challenge = !!document.querySelector('#px-captcha, #sec-if-cpt-container, #sec-cpt-if') || /press (&|and) hold|challenge validation/i.test(t);
  return blocked ? 'blocked' : challenge ? 'challenge' : 'page';
})()"""


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(p, s, network, profile=None):
    body = {"proxy": NETWORKS[network]["proxy"], "headless": s["headless"], "record": False}
    if profile:
        body["profile"] = profile
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return getattr(p, s["type"]).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


# Akamai's _abck carries a status between its first two "~": -1 until a sensor post is accepted. Only that
# field is kept, never the cookie itself. HUMAN issues _px3 once its collector accepts the browser.
def signals(context):
    jar = context.cookies()
    abck = next((c["value"] for c in jar if c["name"] == "_abck"), None)
    parts = abck.split("~") if abck else None
    return {"abck": (parts[1] if len(parts) > 1 else "?") if parts else None, "px3": any(c["name"] == "_px3" for c in jar)}


def state(tab):
    try:
        return tab.evaluate(STATE)
    except Exception:
        return None  # while the page navigates


# One load, then watch the page once a second for 10 s without touching it, so the vendor's script
# can post its sensor data; a network failure is retried once.
def visit(context, key):
    page = PAGES[key]
    tab = context.new_page()
    posts = [0]

    def on_request(r):
        try:
            if r.method == "POST" and page["sensor"](r.url):
                posts[0] += 1
        except Exception:
            pass

    tab.on("request", on_request)
    status = None
    try:
        try:
            res = tab.goto(page["url"], wait_until="domcontentloaded", timeout=45000)
        except Exception:
            res = tab.goto(page["url"], wait_until="domcontentloaded", timeout=45000)
        status = res.status if res else None
    except Exception:
        tab.close()
        return {"outcome": "error"}
    at_load = signals(context)
    seen = set()
    for _ in range(10):
        s = state(tab)
        if s:
            seen.add(s)
        if s == "blocked":
            break
        time.sleep(1)
    last = state(tab)
    if "blocked" in seen or last == "blocked":
        outcome = "blocked"
    elif last == "challenge":
        outcome = "challenge"
    else:
        outcome = "cleared" if "challenge" in seen else "content"
    after = signals(context)
    tab.close()
    cell = {"outcome": outcome, "status": status, "sensor_posts": posts[0]}
    if key == "akamai":
        cell.update(abck_at_load=at_load["abck"], abck_after=after["abck"])
    else:
        cell["px3"] = after["px3"]
    return cell


# One session per setup and network; a fresh context per page so every load is a first visit.
def row(p, setup_key, network, pages):
    s = SETUPS[setup_key]
    browser = launch(p, s, network)
    out = {"setup": s["setup"], "network": NETWORKS[network]["label"]}
    try:
        for key in pages:
            context = browser.new_context()
            out[key] = visit(context, key)
            context.close()
    finally:
        browser.close()
    return out


def api(method, path):
    try:
        return requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=60).json()
    except ValueError:
        return {}


def profile_info(name):
    return next((x for x in api("GET", "/v1/me/profiles").get("profiles", []) if x["name"] == name), None)


# A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
def returning(p, setup_key, network, key):
    name = f"case67-{RUN}"
    visits = []
    for _ in range(2):
        browser = launch(p, SETUPS[setup_key], network, name)
        try:
            visits.append(visit(browser.contexts[0], key))  # the profile's own context; a new_context() is not saved
        finally:
            browser.close()
        # The node saves the profile after the browser exits: wait until it is free again.
        for _ in range(60):
            info = profile_info(name)
            if info and info["in_use"] is False:
                break
            time.sleep(2)
    api("DELETE", f"/v1/me/profiles/{name}")
    return {"setup": SETUPS[setup_key]["setup"], "network": NETWORKS[network]["label"], "page": key, "first": visits[0], "second": visits[1]}


with sync_playwright() as p:
    matrix = [row(p, setup_key, network, list(PAGES)) for network in ("own", "tier1") for setup_key in ("chromium", "chrome", "edge", "camoufox")]
    tier4 = [row(p, setup_key, "tier4", list(PAGES)) for setup_key in ("chrome", "camoufox")]
    profile = returning(p, "chrome", "tier1", "akamai")

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "matrix": matrix, "tier4": tier4, "profile": profile}, indent=2))
