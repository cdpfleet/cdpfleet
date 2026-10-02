# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs)
import json
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
NEXT_PROXY = os.environ["SOCKS_PROXIES"].split(",")[0]

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "proxy_updatable": True, "headless": True}, timeout=60)
res.raise_for_status()
session = res.json()


def swap_proxy(proxy):
    # Swap the proxy on the session's router (the host in wsUrl). Takes effect for new
    # connections; the browser, its tabs, cookies and storage stay as they are.
    r = requests.post(f"https://{urlparse(session['wsUrl']).netloc}/admin/session/proxy",
                      headers={"x-api-key": KEY}, json={"session_id": session["sessionId"], "proxy": proxy}, timeout=30)
    r.raise_for_status()


def exit_ip(page, host):
    return page.goto(f"https://{host}/?format=json", timeout=60000).json()["ip"]


def cookies(page):
    return page.goto("https://httpbin.org/cookies", timeout=60000).json()["cookies"]


with sync_playwright() as p:
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        context = browser.new_context()
        page = context.new_page()
        page.goto("https://httpbin.org/cookies/set?cart=3-items&login=alice", timeout=60000)
        page.evaluate("localStorage.setItem('draft', 'half-written review')")
        before = {"exit_ip": exit_ip(page, "api.ipify.org"), "cookies": cookies(page)}

        t = time.time()
        swap_proxy(NEXT_PROXY)
        swap_ms = round((time.time() - t) * 1000)

        # 1. Same tab, same host: the open keep-alive connection still goes through the old proxy.
        reused_connection = exit_ip(page, "api.ipify.org")
        # 2. Same tab, a host we haven't connected to yet: a new connection, so the new proxy.
        new_connection = exit_ip(page, "api64.ipify.org")
        # 3. Move everything over: a new context (its own connection pool) with the old
        #    cookies and localStorage.
        moved = browser.new_context(storage_state=context.storage_state())
        context.close()
        page2 = moved.new_page()
        after = {
            "exit_ip": exit_ip(page2, "api.ipify.org"),
            "cookies": cookies(page2),
            "local_storage": page2.evaluate("localStorage.getItem('draft')"),
        }
        print(json.dumps({
            "before": before,
            "swap_ms": swap_ms,
            "same_tab_reused_connection": reused_connection,
            "same_tab_new_connection": new_connection,
            "new_context_with_storage_state": after,
            "same_browser_session": browser.is_connected(),
        }, indent=2))
    finally:
        browser.close()
