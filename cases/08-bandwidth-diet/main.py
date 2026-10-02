# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGE = "https://www.bbc.com/news"
FIRST_PARTY = ("bbc.com", "bbc.co.uk", "bbci.co.uk")  # the site's own domains and CDNs
PRICE_PER_GB = 3  # a typical residential proxy price, USD
HEAVY = {"image", "media", "font"}

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": True}, timeout=60)
res.raise_for_status()


def measure(browser, label, block):
    """Load the page in a fresh context (empty cache) and count every byte on the wire."""
    context = browser.new_context()
    if block:
        def handle(route):
            r = route.request
            third_party = not urlparse(r.url).hostname.endswith(FIRST_PARTY)
            if r.resource_type in HEAVY or (block == "first-party" and third_party):
                return route.abort()
            return route.continue_()
        context.route("**/*", handle)
    page = context.new_page()
    stats = {"bytes": 0, "requests": 0, "blocked": 0}

    def finished(req):
        s = req.sizes()
        stats["bytes"] += s["requestHeadersSize"] + s["requestBodySize"] + s["responseHeadersSize"] + s["responseBodySize"]
        stats["requests"] += 1

    page.on("requestfinished", finished)
    page.on("requestfailed", lambda req: stats.__setitem__("blocked", stats["blocked"] + 1))
    t = time.time()
    # DOM ready, then a fixed 5 s for the rest to arrive: the same window for every variant
    # ('load' can wait forever on a blocked video).
    page.goto(PAGE, wait_until="domcontentloaded", timeout=90000)
    ready_ms = round((time.time() - t) * 1000)
    page.wait_for_timeout(5000)
    title = page.title()
    page.remove_listener("requestfinished", finished)  # stop counting before closing
    context.close()
    return {"variant": label, "title": title, "requests": stats["requests"], "blocked": stats["blocked"],
            "kilobytes": round(stats["bytes"] / 1024), "dom_ready_ms": ready_ms}


with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = [
            measure(browser, "everything", None),
            measure(browser, "no images, media or fonts", "heavy"),
            measure(browser, "…and first-party only", "first-party"),
        ]
        full = rows[0]["kilobytes"]
        for r in rows:
            r["saved"] = f"{round((1 - r['kilobytes'] / full) * 100)}%"
            r["proxy_cost_per_100k_pages"] = f"${r['kilobytes'] * 100000 / 1024 / 1024 * PRICE_PER_GB:.0f}"
        print(json.dumps(rows, indent=2, ensure_ascii=False))
    finally:
        browser.close()
