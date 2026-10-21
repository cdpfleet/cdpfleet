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
# Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
NETWORKS = {
    "own": {"label": "own proxy (rotating)", "proxy": os.environ["PROXY_URL"]},
    "tier1": {"label": "cdpfleet-resi Tier 1, sticky US", "proxy": f"cdpfleet-resi-country-us-session-c64{RUN}-lifetime-30"},
    "tier4": {"label": "cdpfleet-resi Tier 4, sticky US", "proxy": f"cdpfleet-resi-tier-4-country-us-session-c64{RUN}-lifetime-30"},
}
SETUPS = {
    "chromium": {"setup": "Chromium, headless", "engine": "chromium", "type": "chromium", "headless": True},
    "chrome": {"setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "headless": False},
    "edge": {"setup": "Microsoft Edge, headful", "engine": "edge", "type": "chromium", "headless": False},
    "camoufox": {"setup": "Camoufox, defaults", "engine": "camoufox", "type": "firefox", "headless": True},
}
# Public test pages only: two Cloudflare challenge pages and a Turnstile test page in two widget modes.
# `cf` pages may answer with Cloudflare's interstitial; `widget` pages embed Turnstile and get a token if it passes.
PAGES = {
    "nowsecure": {"url": "https://nowsecure.nl/", "kind": "cf"},
    "scrapingcourse": {"url": "https://www.scrapingcourse.com/cloudflare-challenge", "kind": "cf"},
    "ts_noninteractive": {"url": "https://peet.ws/turnstile-test/non-interactive.html", "kind": "widget"},
    "ts_managed": {"url": "https://peet.ws/turnstile-test/managed.html", "kind": "widget"},
}

# What the page shows right now, in the lab's words. The same expression runs in every language.
STATE = r"""(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const el = document.querySelector('[name="cf-turnstile-response"]');
  const state = /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
  return { state, token: el && el.value ? el.value.length : 0 };
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


def has_clearance(context):
    return any(c["name"] == "cf_clearance" for c in context.cookies())


# One load, then watch the page once a second without touching it: an interstitial gets 20 s to
# clear by itself, a Turnstile widget 12 s to issue a token. A network failure is retried once.
def visit(context, key):
    page = PAGES[key]
    tab = context.new_page()
    t0 = time.time()
    cell = {"outcome": "challenge", "seconds": None}
    try:
        try:
            tab.goto(page["url"], wait_until="domcontentloaded", timeout=45000)
        except Exception:
            tab.goto(page["url"], wait_until="domcontentloaded", timeout=45000)
    except Exception:
        tab.close()
        return {"outcome": "error", "seconds": None}
    saw_challenge = False
    deadline = t0 + (12 if page["kind"] == "widget" else 20)
    while time.time() < deadline:
        try:
            s = tab.evaluate(STATE)
        except Exception:
            s = None  # while the interstitial reloads the page
        state = s["state"] if s else None
        if state == "blocked":
            cell["outcome"] = "blocked"
            break
        if state == "challenge":
            saw_challenge = True
        if s and page["kind"] == "widget" and s["token"] > 0:
            cell["outcome"], cell["seconds"] = "cleared", round(time.time() - t0)
            break
        if state == "page" and page["kind"] == "cf":
            cell["outcome"] = "cleared" if saw_challenge else "content"
            if saw_challenge:
                cell["seconds"] = round(time.time() - t0)
            break
        time.sleep(1)
    if page["kind"] == "cf":
        # The clearance cookie can arrive a moment after the content: give it up to 6 s.
        for _ in range(6):
            if cell["outcome"] == "blocked" or has_clearance(context):
                break
            time.sleep(1)
        cell["cf_clearance"] = has_clearance(context)
    else:
        cell["token"] = cell["outcome"] == "cleared"
    tab.close()
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
    name = f"case64-{RUN}"
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
    tier4 = [row(p, setup_key, "tier4", ["nowsecure", "scrapingcourse", "ts_managed"]) for setup_key in ("chrome", "camoufox")]
    profile = returning(p, "camoufox", "tier1", "scrapingcourse")

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "matrix": matrix, "tier4": tier4, "profile": profile}, indent=2))
