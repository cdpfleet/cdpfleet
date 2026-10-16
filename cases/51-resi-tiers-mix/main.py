# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (no proxy of your own needed: the cdpfleet residential proxy, billed per GB by tier)
import json
import math
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGE = "https://commons.wikimedia.org/wiki/Category:Cats"
PRICE = {1: 5, 4: 0.5}  # USD per GB (10^9 bytes)
CDN = ["thumb.wikimedia.org", "upload.wikimedia.org"]  # the images; HTML, CSS and JS come from commons.wikimedia.org

# One session, two prices: the page through Tier 1, the image hosts through Tier 4.
LAUNCH = {
    "headless": "new",
    "record": False,
    "proxy": "cdpfleet-resi-country-us",
    "proxy_rules": [{"hosts": CDN, "proxy": "cdpfleet-resi-tier-4-country-us"}],
}


def rnd(x):
    return math.floor(x + 0.5)


# Today's residential traffic per tier, as billed.
def used_today():
    b = requests.get("https://cdpfleet.com/v1/me/proxy-balance", headers={"x-api-key": KEY}, timeout=30).json()
    return {t["tier"]: t["bytes"] for t in b["used_today"]}


# Load the gallery, scroll to the end, and add up every request's bytes by host group.
def load(pw):
    r = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=LAUNCH, timeout=120)
    if not r.ok:
        raise RuntimeError(f"launch {r.status_code} {r.text}")
    browser = pw.chromium.connect(r.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        done = []
        page.on("requestfinished", lambda req: done.append(req))
        # Residential peers drop the odd connection: retry the navigation once.
        for attempt in (1, 2):
            try:
                page.goto(PAGE, wait_until="load", timeout=60000)
                break
            except Exception:
                if attempt == 2:
                    raise
        for _ in range(20):  # lazy-loaded thumbnails load as they scroll into view
            page.mouse.wheel(0, 1500)
            page.wait_for_timeout(250)
        try:
            page.wait_for_load_state("networkidle", timeout=15000)
        except Exception:
            pass
        page_bytes = image_bytes = images = 0
        for req in done:
            try:
                s = req.sizes()
            except Exception:
                continue
            total = s["requestHeadersSize"] + s["requestBodySize"] + s["responseHeadersSize"] + s["responseBodySize"]
            if urlparse(req.url).hostname in CDN:
                image_bytes += total
                images += 1
            else:
                page_bytes += total
        return {"requests": len(done), "image_requests": images, "page_kb": rnd(page_bytes / 1000), "image_kb": rnd(image_bytes / 1000)}
    finally:
        browser.close()


before = used_today()
with sync_playwright() as pw:
    run = load(pw)

# Metering trails the traffic by about a minute: wait until Tier 4 shows the images.
after = before
for _ in range(18):
    if (after.get(4, 0) - before.get(4, 0)) / 1000 >= run["image_kb"] * 0.8:
        break
    time.sleep(10)
    after = used_today()


# What 1,000 such loads cost: everything on Tier 1 versus the split.
def per1000(page_tier, image_tier):
    return rnd((run["page_kb"] * PRICE[page_tier] + run["image_kb"] * PRICE[image_tier]) / 1e6 * 1000 * 100) / 100


print(json.dumps({
    "page": PAGE,
    "run": run,
    "image_share": rnd(run["image_kb"] / (run["image_kb"] + run["page_kb"]) * 100) / 100,
    "cost_per_1000_loads_usd": {"all_tier_1": per1000(1, 1), "page_tier_1_images_tier_4": per1000(1, 4)},
    "billed_kb": {
        "tier_1": rnd((after.get(1, 0) - before.get(1, 0)) / 1000),
        "tier_4": rnd((after.get(4, 0) - before.get(4, 0)) / 1000),
    },
}, indent=2))
