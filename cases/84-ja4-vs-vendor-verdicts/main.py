# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY
import json
import os
import secrets
import time
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RUN = secrets.token_hex(3)
# The same twelve setups as the real-site lab cases 70-77 (Rebrowser needs a 1.52 client, so it's left out).
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


def launch(engine, headless, proxy):
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY},
                            json={"proxy": proxy, "headless": headless, "record": False}, timeout=120)
        if res.ok:
            return res.json()["wsUrl"]
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


def from_peet(j):
    tls, h2 = j.get("tls") or {}, j.get("http2") or {}
    return {"http_version": j.get("http_version"), "ja4": tls.get("ja4"), "ja4_r": tls.get("ja4_r"), "peetprint_hash": tls.get("peetprint_hash"),
            "akamai_h2": h2.get("akamai_fingerprint"), "akamai_h2_hash": h2.get("akamai_fingerprint_hash"), "user_agent": j.get("user_agent")}


def from_browserleaks(j):
    return {"http_version": None, "ja4": j.get("ja4"), "ja4_r": j.get("ja4_r") or None, "peetprint_hash": None,
            "akamai_h2": j.get("akamai_text") or None, "akamai_h2_hash": j.get("akamai_hash") or None, "user_agent": j.get("user_agent")}


# The browser itself loads a TLS echo page, which answers with the ClientHello and HTTP/2 frames
# it received from this very browser. tls.peet.ws first; tls.browserleaks.com/json only if peet
# doesn't answer (it has had outages). Each is one page load in a fresh tab.
ECHOES = [
    ("tls.peet.ws", "https://tls.peet.ws/api/all", from_peet),
    ("tls.browserleaks.com", "https://tls.browserleaks.com/json", from_browserleaks),
]


def fingerprint(browser):
    context = browser.new_context()
    last = None
    for source, url, read in ECHOES:
        tab = context.new_page()
        try:
            res = tab.goto(url, wait_until="domcontentloaded", timeout=30000)
            fp = read(json.loads(res.text()))
            if fp["ja4"]:
                return {"source": source, **fp}
            last = f"{source}: no ja4 in the answer"
        except Exception as err:
            last = f"{source}: {str(err).splitlines()[0]}"
        finally:
            try:
                tab.close()
            except Exception:
                pass
    raise RuntimeError(last)


# One sticky residential exit for the whole run, and one session at a time.
proxy = f"cdpfleet-resi-country-us-session-c84{RUN}-lifetime-30"
fingerprints = {}
with sync_playwright() as pw:
    for sid, _, engine, kind, headless in SETUPS:
        try:
            browser = getattr(pw, kind).connect(launch(engine, headless, proxy), headers={"x-api-key": KEY})
            try:
                fingerprints[sid] = {"version": browser.version, **fingerprint(browser)}
            finally:
                browser.close()
        except Exception as err:
            fingerprints[sid] = {"error": str(err).splitlines()[0][:200]}

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
                  "setups": {s[0]: s[1] for s in SETUPS}, "fingerprints": fingerprints}, indent=2))
