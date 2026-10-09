# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
START = "https://books.toscrape.com/"

EXTRACT_JS = """() => [...document.querySelectorAll('article.product_pod')].map(el => {
  const stars = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };
  const ratingClass = [...el.querySelector('.star-rating').classList].find(c => c !== 'star-rating');
  return {
    title: el.querySelector('h3 a').getAttribute('title'),
    price: parseFloat(el.querySelector('.price_color').textContent.replace(/[^0-9.]/g, '')),
    rating: stars[ratingClass] || 0,
    in_stock: el.querySelector('.availability').textContent.trim().toLowerCase().includes('in stock'),
  };
})"""

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        t = time.time()
        books = []

        page.goto(START, timeout=60000)
        books.extend(page.evaluate(EXTRACT_JS))

        nxt = page.locator("li.next a")
        if nxt.count() > 0:
            nxt.click()
            page.wait_for_load_state("domcontentloaded")
            books.extend(page.evaluate(EXTRACT_JS))

        print(json.dumps({
            "pages_scraped": 2,
            "total_books": len(books),
            "books": books,
            "seconds": round(time.time() - t, 2),
        }, indent=2, ensure_ascii=False))
    finally:
        browser.close()
