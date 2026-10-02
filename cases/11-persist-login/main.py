# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import tempfile

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
STATE_FILE = os.path.join(tempfile.gettempdir(), "cdpfleet-state.json")  # keep it safe: it holds the login


def launch(name):
    res = requests.post(f"https://starter.cdpfleet.com/{name}/session", headers={"x-api-key": KEY},
                        json={"proxy": os.environ["PROXY_URL"], "headless": True}, timeout=60)
    res.raise_for_status()
    return res.json()


def cookies_seen(page):
    return page.goto("https://httpbin.org/cookies", timeout=60000).json()["cookies"]


with sync_playwright() as p:
    # Session 1 (Chromium): "log in", then save cookies + localStorage to a local file.
    s1 = launch("chromium")
    b1 = p.chromium.connect(s1["wsUrl"], headers={"x-api-key": KEY})
    try:
        ctx = b1.new_context()
        page = ctx.new_page()
        page.goto("https://httpbin.org/cookies/set?session=abc123&user=alice", timeout=60000)
        page.evaluate("localStorage.setItem('draft', 'half-written review')")
        saved = ctx.storage_state(path=STATE_FILE)
    finally:
        b1.close()  # the browser is gone; only the state file remains

    # Session 2 (Firefox, a fresh browser on whichever server the fleet picks): restore it.
    s2 = launch("firefox")
    b2 = p.firefox.connect(s2["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = b2.new_context(storage_state=STATE_FILE).new_page()
        cookies = cookies_seen(page)
        draft = page.evaluate("localStorage.getItem('draft')")
        blank_cookies = cookies_seen(b2.new_context().new_page())  # the same browser without the state
        print(json.dumps({
            "session_1": {"id": s1["sessionId"], "browser": "chromium",
                          "saved_cookies": [f"{c['name']}@{c['domain']}" for c in saved["cookies"]],
                          "saved_origins": [o["origin"] for o in saved["origins"]]},
            "state_file_bytes": os.path.getsize(STATE_FILE),
            "session_2": {"id": s2["sessionId"], "browser": "firefox", "cookies_sent": cookies, "local_storage_draft": draft},
            "session_2_without_state": {"cookies_sent": blank_cookies},
        }, indent=2))
    finally:
        b2.close()
