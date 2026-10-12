# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import base64
import hashlib
import hmac
import json
import os
import re
import struct
import time
from email.utils import parsedate_to_datetime
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITE = "https://authenticationtest.com"
# The practice site publishes its test account and TOTP secret on the challenge page.
LOGIN = {"email": "totp@authenticationtest.com", "password": "pa$$w0rd", "secret": "I65VU7K5ZQL7WB4E"}


def totp(secret, unix_seconds):
    """RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps) from the base32 secret — what an authenticator app does."""
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    h = hmac.new(key, struct.pack(">Q", int(unix_seconds // 30)), hashlib.sha1).digest()
    o = h[-1] & 15
    return "%06d" % ((struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000)


def fresh_window():
    """A code typed in the last seconds of its window can expire before the server checks it:
    wait for the next window when fewer than 5 s are left."""
    left = 30 - time.time() % 30
    if left < 5:
        time.sleep(left + 0.2)
    return {"seconds_left_in_window": round(left, 1), "waited_s": round(left, 1) if left < 5 else 0}


def launch(p):
    res = requests.post("https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": True}, timeout=60)
    if not res.ok:
        raise SystemExit(f"launch {res.status_code} {res.text}")
    session = res.json()
    return session["sessionId"], p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})


def attempt(browser, label, offset):
    """One login attempt in a fresh context (no cookies from earlier attempts). The code is made
    after the form has loaded, right before typing it: offset None types a wrong code, otherwise
    the code for now + offset seconds."""
    context = browser.new_context()
    page = context.new_page()
    page.goto(f"{SITE}/totpChallenge/", timeout=60000)
    window = fresh_window()
    code = "000000" if offset is None else totp(LOGIN["secret"], time.time() + offset)
    t = time.time()
    page.fill("#email", LOGIN["email"])
    page.fill("#password", LOGIN["password"])
    page.fill("#totpmfa", code)
    page.click("input[type=submit]")
    page.wait_for_url(re.compile(r"/login(Success|Fail)/"), timeout=60000)
    landed = urlparse(page.url).path
    return context, {"attempt": label, "offset_s": offset, **window, "landed": landed,
                     "accepted": landed == "/loginSuccess/", "ms": round((time.time() - t) * 1000)}


out = {"site": f"{SITE}/totpChallenge/", "session_ids": {}, "clocks": {}, "attempts": [], "storage_state": None, "second_session": None}
with sync_playwright() as p:
    out["session_ids"]["first"], browser = launch(p)
    try:
        # What matters is your clock against the server's. The site's Date header (from its CDN
        # edge, 1 s resolution) should fall between our clock just before and just after the
        # request. The remote browser's clock is recorded for comparison only.
        page = browser.new_page()
        t0 = time.time()
        resp = page.goto(f"{SITE}/totpChallenge/", timeout=60000)
        t1 = time.time()
        server = parsedate_to_datetime(resp.headers["date"]).timestamp()
        out["clocks"]["server_date_within_request"] = int(t0) - 1 <= server <= t1 + 1
        out["clocks"]["request_s"] = round(t1 - t0, 1)
        out["clocks"]["browser_minus_local_s"] = round(page.evaluate("Date.now()") / 1000 - time.time(), 1) or 0.0
        page.close()

        for label, offset in [("wrong code", None), ("code from 2 windows ago (-60 s)", -60),
                              ("code from the previous window (-30 s)", -30), ("code from the next window (+30 s)", 30)]:
            context, result = attempt(browser, label, offset)
            out["attempts"].append(result)
            context.close()
        context, result = attempt(browser, "current code", 0)
        out["attempts"].append(result)
        state = context.storage_state()
        out["storage_state"] = {"cookies": len(state["cookies"]), "session_cookies": len([c for c in state["cookies"] if c["expires"] == -1])}
    finally:
        browser.close()

    # A new session (another browser): without the saved state, then with it.
    out["session_ids"]["second"], second = launch(p)
    try:
        out["second_session"] = {}
        for label, options in [("without_state", {}), ("with_state", {"storage_state": state})]:
            context = second.new_context(**options)
            page = context.new_page()
            page.goto(f"{SITE}/", timeout=60000)
            out["second_session"][label] = {"signed_in": page.locator("text=Sign Out").count() > 0}
            context.close()
    finally:
        second.close()

print(json.dumps(out, indent=2))
