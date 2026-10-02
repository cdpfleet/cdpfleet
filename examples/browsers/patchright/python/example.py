# pip install playwright==1.60.0 requests
# Browsers run on cdpfleet, so no `playwright install` is needed.
import os
import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# 1. Launch the browser
res = requests.post(
    "https://starter.cdpfleet.com/patchright/session",
    headers={"x-api-key": KEY},
    json={
        "proxy": "http://user:pass@proxy.example.com:8080",
        "headless": "new",
    },
    timeout=60,
)
res.raise_for_status()
ws_url = res.json()["wsUrl"]

# 2. Connect and drive it
with sync_playwright() as p:
    browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
    page = browser.new_page()
    page.goto("https://example.com")
    print(page.title())
    browser.close()  # ends the session and stops billing
