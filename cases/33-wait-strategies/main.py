# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
# A static catalogue, a big article and a page that renders its data with a delayed script.
PAGES = [
    {"url": "https://books.toscrape.com/", "data": "article.product_pod"},
    {"url": "https://en.wikipedia.org/wiki/Web_scraping", "data": "#mw-content-text p"},
    {"url": "https://quotes.toscrape.com/js-delayed/", "data": ".quote"},
]
STRATEGIES = ["commit", "domcontentloaded", "load", "networkidle", "selector"]

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = []
        for pg in PAGES:
            url, data = pg["url"], pg["data"]
            for strategy in STRATEGIES:
                # A fresh context each time: no cache, so every strategy waits for the same work.
                context = browser.new_context()
                page = context.new_page()
                t = time.time()
                if strategy == "selector":
                    page.goto(url, wait_until="commit", timeout=60000)
                    page.locator(data).first.wait_for(timeout=60000)
                else:
                    page.goto(url, wait_until=strategy, timeout=60000)
                ms = round((time.time() - t) * 1000)
                rows.append({"url": url, "strategy": strategy, "ms": ms, "items_ready": page.locator(data).count()})
                context.close()

        print(json.dumps({"rows": rows}, indent=2, ensure_ascii=False))
    finally:
        browser.close()
