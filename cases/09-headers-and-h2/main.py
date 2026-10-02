# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

res = requests.post("https://starter.cdpfleet.com/chrome/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": False}, timeout=60)
res.raise_for_status()


def observe(browser, label, context_options, setup=None):
    """What the server saw: header names in order, and the HTTP/2 fingerprint."""
    context = browser.new_context(**context_options)
    page = context.new_page()
    if setup:
        setup(page)
    fp = page.goto("https://tls.peet.ws/api/all", timeout=60000).json()
    context.close()
    headers = next(f for f in fp["http2"]["sent_frames"] if f["frame_type"] == "HEADERS")["headers"]
    return {
        "variant": label,
        "header_order": [h[:h.index(":", 1)] for h in headers],
        "accept_language": next((h[17:] for h in headers if h.startswith("accept-language: ")), None),
        "akamai_h2": fp["http2"]["akamai_fingerprint"],
        "ja4": fp["tls"]["ja4"],
    }


def rewrite(page):
    page.route("**/*", lambda route: route.continue_(headers={**route.request.headers, "x-request-id": "abc123"}))


with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        print(json.dumps([
            observe(browser, "default", {}),
            observe(browser, "locale: de-DE", {"locale": "de-DE"}),
            observe(browser, "extraHTTPHeaders", {"extra_http_headers": {"accept-language": "de-DE", "x-request-id": "abc123"}}),
            observe(browser, "route: rewrite headers", {}, rewrite),
        ], indent=2))
    finally:
        browser.close()
