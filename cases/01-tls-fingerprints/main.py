# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (http://user:pass@host:port)
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]

# One browser per engine family, and the Playwright client that speaks to it.
BROWSERS = [("chrome", "chromium"), ("edge", "chromium"), ("firefox", "firefox"),
            ("camoufox", "firefox"), ("webkit", "webkit")]


def launch(name, options):
    res = requests.post(f"https://starter.cdpfleet.com/{name}/session",
                        headers={"x-api-key": KEY}, json=options, timeout=60)
    res.raise_for_status()
    return res.json()


def fingerprint(browser):
    # Navigate the browser itself: the JSON describes the TLS ClientHello and HTTP/2
    # frames this very browser sent (page.request would use Playwright's own client).
    # A residential exit occasionally times out: one retry, in a fresh tab.
    for attempt in (1, 2):
        try:
            return browser.new_page().goto("https://tls.peet.ws/api/all", timeout=30000).json()
        except Exception:
            if attempt == 2:
                raise


rows = []
with sync_playwright() as p:
    for name, family in BROWSERS:
        session = launch(name, {"proxy": PROXY, "headless": True})
        browser = getattr(p, family).connect(session["wsUrl"], headers={"x-api-key": KEY})
        try:
            fp = fingerprint(browser)
            http2 = fp.get("http2") or {}
            rows.append({
                "browser": name,
                "version": browser.version,
                "user_agent": fp["user_agent"],
                "http_version": fp["http_version"],
                "ja4": fp["tls"]["ja4"],
                "ja3_hash": fp["tls"]["ja3_hash"],
                "peetprint_hash": fp["tls"]["peetprint_hash"],
                "akamai_h2": http2.get("akamai_fingerprint"),
                "akamai_h2_hash": http2.get("akamai_fingerprint_hash"),
                "cipher_suites": len(fp["tls"]["ciphers"]),
                "extensions": len(fp["tls"]["extensions"]),
            })
        finally:
            browser.close()  # ends the session and stops billing

print(json.dumps(rows, indent=2))
