# pip install playwright==1.60.0 requests
# Browsers run on cdpfleet, so no `playwright install` is needed.
import os
import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# 1. Launch the browser
res = requests.post(
    "https://starter.cdpfleet.com/chromium/session",
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
    # Live view: JPEG frames whenever the page changes (Playwright 1.60+)
    page.screencast.start(quality=60, on_frame=lambda frame: print(f"frame: {len(frame['data'])} bytes"))
    page.goto("https://example.com")
    print(page.title())
    page.screencast.stop()
    browser.close()  # ends the session and stops billing
