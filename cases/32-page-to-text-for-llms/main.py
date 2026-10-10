# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URLS = ["https://news.ycombinator.com/", "https://en.wikipedia.org/wiki/Web_scraping", "https://books.toscrape.com/"]

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        pages = []
        for url in URLS:
            page.goto(url, wait_until="domcontentloaded", timeout=60000)
            html = page.content()
            text = page.locator("body").inner_text()
            aria = page.locator("body").aria_snapshot()
            pages.append({
                "url": url,
                "html_chars": len(html),
                "text_chars": len(text),
                "aria_chars": len(aria),
                "aria_links": aria.count("- link "),
                "aria_vs_html": round(len(aria) / len(html) * 100, 1),
                "aria_sample": "\n".join(aria.split("\n")[:6]),
            })
        print(json.dumps({"pages": pages}, indent=2, ensure_ascii=False))
    finally:
        browser.close()
