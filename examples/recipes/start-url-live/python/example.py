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
        "headless": True,
        "live_view": True,
        "start_url": "https://books.toscrape.com/",
    },
    timeout=60,
)
res.raise_for_status()
ws_url = res.json()["wsUrl"]

# 2. Connect and drive it
with sync_playwright() as p:
    browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
    context = browser.contexts[0]  # the page start_url opened
    page = context.pages[0] if context.pages else context.new_page()
    page.wait_for_url("http*://**")  # until the start page has loaded
    print(page.title())
    browser.close()  # ends the session and stops billing
