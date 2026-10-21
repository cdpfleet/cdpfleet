# pip install playwright==1.60.0 requests
# Browsers run on cdpfleet, so no `playwright install` is needed.
import os
import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# 1. Launch the browser
res = requests.post(
    "https://starter.cdpfleet.com/camoufox/session",
    headers={"x-api-key": KEY},
    json={
        "proxy": "http://user:pass@proxy.example.com:8080",
        "locale": "en-GB",
        "fingerprint": {
            "navigator": {
                "userAgent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:152.0) Gecko/20100101 Firefox/152.0",
                "platform": "MacIntel",
                "hardwareConcurrency": 8,
                "maxTouchPoints": 0,
            },
            "screen": {
                "width": 1512,
                "height": 982,
                "availWidth": 1512,
                "availHeight": 944,
                "colorDepth": 30,
                "pixelDepth": 30,
            },
        },
    },
    timeout=60,
)
res.raise_for_status()
ws_url = res.json()["wsUrl"]

# 2. Connect and drive it
with sync_playwright() as p:
    browser = p.firefox.connect(ws_url, headers={"x-api-key": KEY})
    page = browser.new_page()
    page.goto("https://example.com")
    print(page.title())
    browser.close()  # ends the session and stops billing
