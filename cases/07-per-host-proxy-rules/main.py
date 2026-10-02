# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs, 3 or more)
import json
import os
import re
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
socks_a, socks_b, socks_c = os.environ["SOCKS_PROXIES"].split(",")[:3]
host_of = lambda url: urlparse(url).hostname

options = {
    "proxy": os.environ["PROXY_URL"],  # everything not matched below
    "proxy_rules": [
        {"hosts": ["*.ident.me", "ident.me"], "proxy": socks_a},
        {"hosts": ["httpbin.org", "*.httpbin.org"], "proxy": [socks_b, socks_c]},  # round-robin
        # IP-lookup services always use the default proxy, so this rule is ignored on purpose.
        {"hosts": ["api.ipify.org"], "proxy": socks_a},
    ],
    "headless": True,
}
res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=options, timeout=60)
res.raise_for_status()


def exit_ip_via(browser, url):
    # Each context has its own connection pool, so each reading is a fresh connection.
    # Proxies drop a connection now and then: one retry.
    for attempt in (1, 2):
        context = browser.new_context()
        try:
            text = context.new_page().goto(url, timeout=30000).text()
            m = re.search(r"\d{1,3}(\.\d{1,3}){3}", text)
            return m.group(0) if m else f"(no IP in {url})"
        except Exception as err:
            if attempt == 2:
                return f"(failed: {str(err).splitlines()[0]})"
        finally:
            context.close()


with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        urls = ["https://v4.ident.me/", "https://httpbin.org/ip", "https://httpbin.org/ip", "https://httpbin.org/ip",
                "https://api.ipify.org/", "https://www.cloudflare.com/cdn-cgi/trace"]
        print(json.dumps({
            "rules": {
                "*.ident.me": host_of(socks_a),
                "httpbin.org": [host_of(socks_b), host_of(socks_c)],
                "api.ipify.org": f"{host_of(socks_a)} (ignored: IP-lookup host)",
                "(everything else)": "residential PROXY_URL",
            },
            "readings": [{"url": u, "exit_ip": exit_ip_via(browser, u)} for u in urls],
        }, indent=2))
    finally:
        browser.close()
