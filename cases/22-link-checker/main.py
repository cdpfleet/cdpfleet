# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
START = "https://books.toscrape.com/"  # 70-odd same-site links on the front page
WAVE = 8  # links checked at once

LINKS_JS = "(as, origin) => [...new Set(as.map(a => a.href))].filter(h => h.startsWith(origin) && !h.includes('#'))"
FETCH_JS = """urls => Promise.all(urls.map(async url => {
  try { const r = await fetch(url, { cache: 'no-store' }); return { url, status: r.status, redirected: r.redirected }; }
  catch { return { url, status: 0, redirected: false }; }
}))"""


def summary(method, results, seconds, **extra):
    broken = [r for r in results if r["status"] == 0 or r["status"] >= 400]
    return {
        "method": method,
        "links": len(results),
        "ok": sum(1 for r in results if 200 <= r["status"] < 300),
        "redirected": sum(1 for r in results if r["redirected"]),
        "broken": len(broken),
        "broken_urls": [r["url"] for r in broken][:5],
        "seconds": round(seconds, 3),
        "seconds_per_link": round(seconds / len(results), 2),
        **extra,
    }


res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        page.goto(START, timeout=60000)
        # Every unique same-site link on the page, as absolute URLs.
        origin = f"{urlparse(START).scheme}://{urlparse(START).netloc}"
        links = page.eval_on_selector_all("a[href]", LINKS_JS, origin)

        # Way 1: fetch() inside the page, WAVE links at a time — the browser's proxy, cookies
        # and TLS, no navigation, no rendering, no assets.
        t1 = time.time()
        fetched = []
        for i in range(0, len(links), WAVE):
            fetched.extend(page.evaluate(FETCH_JS, links[i:i + WAVE]))
        in_page = summary(f"fetch() in the page, {WAVE} at a time", fetched, time.time() - t1, requests_made=len(links))

        # Way 2: navigate to each link, like a user — full page loads with all their assets.
        # Only a sample: this is the slow way, and it is the same work for every link.
        sample = links[:10]
        requests_ = {"n": 0}
        page.on("request", lambda _: requests_.__setitem__("n", requests_["n"] + 1))
        t2 = time.time()
        navigated = []
        for url in sample:
            try:
                r = page.goto(url, timeout=60000)
                navigated.append({"url": url, "status": r.status if r else 0, "redirected": r.url != url if r else False})
            except Exception:
                navigated.append({"url": url, "status": 0, "redirected": False})
        by_nav = summary("page.goto each link (10-link sample)", navigated, time.time() - t2, requests_made=requests_["n"])

        print(json.dumps([in_page, by_nav], indent=2, ensure_ascii=False))
    finally:
        browser.close()
