# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import tempfile
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGE = "https://books.toscrape.com/"

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p, tempfile.TemporaryDirectory() as td:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page(viewport={"width": 1280, "height": 720})
        page.goto(PAGE, timeout=60000, wait_until="networkidle")

        results = []

        def capture(label, fn):
            ext = ".pdf" if "pdf" in label else ".png"
            path = os.path.join(td, label.replace(" ", "-") + ext)
            t = time.time()
            fn(path)
            size = os.path.getsize(path)
            return {"type": label, "file_size_bytes": size, "seconds": round(time.time() - t, 2)}

        results.append(capture("viewport screenshot",
            lambda p: page.screenshot(path=p)))
        results.append(capture("full-page screenshot",
            lambda p: page.screenshot(path=p, full_page=True)))
        results.append(capture("element screenshot",
            lambda p: page.locator(".product_pod").first.screenshot(path=p)))
        results.append(capture("pdf",
            lambda p: page.pdf(path=p)))

        print(json.dumps({"captures": results}, indent=2))
    finally:
        browser.close()
