# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time

import requests
from playwright.sync_api import Error, sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URL = "https://books.toscrape.com/"
# Chrome DevTools' presets (throughput in bytes/s, latency in ms).
PROFILES = {
    "no throttling": None,
    "Fast 4G": {"latency": 150, "downloadThroughput": 9 * 1024 * 1024 / 8, "uploadThroughput": 1.5 * 1024 * 1024 / 8},
    "Slow 3G": {"latency": 2000, "downloadThroughput": 400 * 1024 / 8, "uploadThroughput": 400 * 1024 / 8},
}


def ms_since(t):
    return round((time.time() - t) * 1000)


res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = []
        for profile, conditions in PROFILES.items():
            context = browser.new_context()  # no cache between runs
            page = context.new_page()
            # A CDP session works over the normal Playwright connection on Chromium-based browsers.
            cdp = context.new_cdp_session(page)
            cdp.send("Network.enable")
            if conditions:
                cdp.send("Network.emulateNetworkConditions", {"offline": False, **conditions})
            t = time.time()
            page.goto(URL, wait_until="domcontentloaded", timeout=120000)
            html_ms = ms_since(t)
            page.wait_for_load_state("load", timeout=180000)
            rows.append({"profile": profile, "html_ms": html_ms, "load_ms": ms_since(t)})
            context.close()

        # Offline: the page fails like it would on a dead connection.
        context = browser.new_context()
        page = context.new_page()
        cdp = context.new_cdp_session(page)
        cdp.send("Network.enable")
        cdp.send("Network.emulateNetworkConditions", {"offline": True, "latency": 0, "downloadThroughput": -1, "uploadThroughput": -1})
        try:
            page.goto(URL, timeout=30000)
            offline = "loaded"
        except Error as e:
            m = re.search(r"net::[A-Z_]+", e.message)
            offline = m.group(0) if m else e.message.split("\n")[0]
        context.close()

        print(json.dumps({"rows": rows, "offline": offline}, indent=2))
    finally:
        browser.close()
