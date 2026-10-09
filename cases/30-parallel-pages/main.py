# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URLS = [
    "https://books.toscrape.com/",
    "https://quotes.toscrape.com/",
    "https://example.com",
    "https://httpbin.org/html",
    "https://www.scrapethissite.com/",
]

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        def extract(url):
            page = browser.new_page()
            try:
                page.goto(url, timeout=60000)
                return {"url": url, "title": page.title()}
            finally:
                page.close()

        t1 = time.time()
        sequential = []
        for url in URLS:
            sequential.append({**extract(url), "method": "sequential"})
        seq_seconds = round(time.time() - t1, 2)

        t2 = time.time()
        with ThreadPoolExecutor(max_workers=len(URLS)) as pool:
            parallel = [{**r, "method": "parallel"} for r in pool.map(extract, URLS)]
        par_seconds = round(time.time() - t2, 2)

        print(json.dumps({
            "sequential_seconds": seq_seconds,
            "parallel_seconds": par_seconds,
            "speedup": f"{round(seq_seconds / par_seconds, 1)}x",
            "results": sequential + parallel,
        }, indent=2, ensure_ascii=False))
    finally:
        browser.close()
