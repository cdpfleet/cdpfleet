# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time
from urllib.parse import unquote, urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = urlparse(os.environ["PROXY_URL"])
UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36"

# Three pages, one question each: is the data in the HTML, or does it need JavaScript?
TARGETS = [
    {"url": "https://books.toscrape.com/", "item": "product_pod", "expected": 20},
    {"url": "https://quotes.toscrape.com/js/", "item": "quote", "expected": 10},
    {"url": "https://quotes.toscrape.com/scroll", "item": "quote", "expected": 10},
]


def count_in_html(html, cls):
    return len(re.findall(rf'class="[^"]*\b{cls}\b[^"]*"', html))


with sync_playwright() as p:
    # Step 1: a plain HTTP GET through the same proxy, no browser (Playwright's request API
    # runs locally; the proxy keeps the exit IP identical to the browser's).
    http = p.request.new_context(
        proxy={"server": f"{PROXY.scheme}://{PROXY.hostname}:{PROXY.port}",
               "username": unquote(PROXY.username or ""), "password": unquote(PROXY.password or "")},
        user_agent=UA,
    )
    rows = []
    for t in TARGETS:
        t0 = time.time()
        res = http.get(t["url"], timeout=60000)
        html = res.text()
        rows.append({"url": t["url"], "http_status": res.status, "html_kb": round(len(html) / 1024),
                     "items_in_html": count_in_html(html, t["item"]), "fetch_seconds": round(time.time() - t0, 3)})
    http.dispose()

    # Step 2: only the pages whose HTML didn't have the items get a browser.
    needs_browser = [r for r, t in zip(rows, TARGETS) if r["items_in_html"] < t["expected"]]
    if needs_browser:
        launch = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                               json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
        if not launch.ok:
            raise SystemExit(f"launch {launch.status_code} {launch.text}")
        browser = p.chromium.connect(launch.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            for r in needs_browser:
                t = next(x for x in TARGETS if x["url"] == r["url"])
                page = browser.new_page()
                t0 = time.time()
                page.goto(r["url"], timeout=60000)
                page.locator(f".{t['item']}").first.wait_for(timeout=60000)
                r["items_in_browser"] = page.locator(f".{t['item']}").count()
                r["browser_seconds"] = round(time.time() - t0, 3)
                page.close()
        finally:
            browser.close()

for r in rows:
    r["needs_browser"] = "items_in_browser" in r
    r.setdefault("items_in_browser", None)
    r.setdefault("browser_seconds", None)
print(json.dumps(rows, indent=2, ensure_ascii=False))
