# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_US
import hashlib
import json
import os
import secrets
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
NAME = f"case49-python-{secrets.token_hex(4)}"  # one profile per run
SITE = "https://httpbin.org/html"
# One country for every launch: Camoufox matches timezone and locale to the exit IP.
BODY = {"proxy": os.environ["PROXY_URL_US"], "headless": True, "os": "windows", "record": False}

FINGERPRINT = """() => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const draw = () => {
    const c = document.createElement('canvas');
    c.width = 240; c.height = 60;
    const ctx = c.getContext('2d');
    ctx.font = '18px Arial'; ctx.fillStyle = '#f60'; ctx.fillRect(10, 10, 100, 30); ctx.fillStyle = '#069'; ctx.fillText('cdpfleet device 49', 4, 40);
    return c.toDataURL();
  };
  return {
    user_agent: navigator.userAgent,
    platform: navigator.platform,
    cores: navigator.hardwareConcurrency,
    screen: `${screen.width}x${screen.height}`,
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    canvas: draw(),
    canvas_again: draw(),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    languages: navigator.languages,
    login_cookie: document.cookie.includes('session=alice-7f3'),
    login_storage: localStorage.getItem('user') === 'alice',
  };
}"""


def short_hash(text, n):
    return hashlib.sha256(text.encode()).hexdigest()[:n]


def api(method, path):
    return requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=30)


# The node saves the profile after the session's browser has exited: wait for the new version.
def wait_for_version(version):
    for _ in range(60):
        p = next((x for x in api("GET", "/v1/me/profiles").json().get("profiles", []) if x["name"] == NAME), None)
        if p and p["version"] >= version and not p["in_use"]:
            return p
        time.sleep(2)
    raise RuntimeError(f"profile {NAME} never reached version {version}")


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(body):
    for attempt in range(1, 6):
        r = requests.post("https://starter.cdpfleet.com/camoufox/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# Proxies drop a tunnel now and then: up to two retries.
def go(page, url):
    for attempt in range(1, 4):
        try:
            return page.goto(url, timeout=60000)
        except Exception:
            if attempt == 3:
                raise
            time.sleep(2)


# What a fingerprinting script reads, plus the "login" kept in a cookie and localStorage.
def visit(page, log_in):
    go(page, SITE)
    if log_in:
        page.evaluate("() => { document.cookie = 'session=alice-7f3; max-age=604800; path=/'; localStorage.setItem('user', 'alice'); }")
    fp = page.evaluate(FINGERPRINT)
    fp["canvas_same_in_session"] = fp["canvas"] == fp["canvas_again"]
    fp["canvas"] = short_hash(fp["canvas"], 12)
    del fp["canvas_again"]
    return fp


def session(pw, body, log_in):
    s = launch(body)
    browser = pw.firefox.connect(s["wsUrl"], headers={"x-api-key": KEY})
    try:
        # With a profile, its context is browser.contexts[0]; without one, a fresh context.
        context = browser.contexts[0] if "profile" in body else browser.new_context()
        page = context.pages[0] if context.pages else context.new_page()
        return visit(page, log_in)
    finally:
        browser.close()


# The device's hardware-like values (user agent, platform, cores, screen, GPU); the canvas is
# reported on its own. JSON like JavaScript's JSON.stringify, so the hash matches across languages.
def device(r):
    return short_hash(json.dumps([r["user_agent"], r["platform"], r["cores"], r["screen"], r["webgl_renderer"]], separators=(",", ":"), ensure_ascii=False), 10)


def pick(run, r):
    return {"run": run, "device": device(r), "user_agent": r["user_agent"], "cores": r["cores"], "screen": r["screen"],
            "webgl_renderer": r["webgl_renderer"], "canvas": r["canvas"], "canvas_same_in_session": r["canvas_same_in_session"],
            "timezone": r["timezone"], "login_cookie": r["login_cookie"], "login_storage": r["login_storage"]}


try:
    with sync_playwright() as pw:
        # Four launches with one profile: the first creates it (device + login), the rest restore it.
        with_profile = []
        for run in range(1, 5):
            with_profile.append(pick(run, session(pw, {**BODY, "profile": NAME}, run == 1)))
            wait_for_version(run)
        # Two launches without a profile, for contrast.
        without = [pick(run, session(pw, BODY, False)) for run in range(1, 3)]
    print(json.dumps({"with_profile": with_profile, "without_profile": without}, indent=2, ensure_ascii=False))
finally:
    api("DELETE", f"/v1/me/profiles/{NAME}")
