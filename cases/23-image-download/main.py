# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import base64
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGE = "https://books.toscrape.com/"  # 20 cover images

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
res.raise_for_status()


def is_jpeg(b):
    return len(b) > 3 and b[0] == 0xFF and b[1] == 0xD8 and b[2] == 0xFF


def summary(method, files, seconds, extra_requests):
    return {
        "method": method, "images": len(files), "valid_jpeg": sum(1 for f in files if is_jpeg(f["bytes"])),
        "total_kb": int(sum(len(f["bytes"]) for f in files) / 1024 + 0.5), "extra_requests": extra_requests, "seconds": seconds,
    }


# Fetch each image again from inside the page and hand the bytes over as base64.
REFETCH = """urls => Promise.all(urls.map(async (url) => {
  const buf = await (await fetch(url, { cache: 'no-store' })).arrayBuffer();
  let s = ''; const b = new Uint8Array(buf); for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
  return { url, b64: btoa(s) };
}))"""

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        # Way 1: keep the bytes the page downloads anyway — zero extra requests. In the sync
        # API the handler only remembers the response; bodies are read after navigation.
        page = browser.new_page()
        image_responses = []
        page.on("response", lambda r: image_responses.append(r) if r.request.resource_type == "image" and r.ok else None)
        t1 = time.time()
        page.goto(PAGE, timeout=60000, wait_until="networkidle")
        covers = page.eval_on_selector_all("article.product_pod img", "imgs => imgs.map(i => i.currentSrc || i.src)")
        captured = []
        for r in image_responses:
            try:
                captured.append({"url": r.url, "bytes": r.body()})
            except Exception:
                pass  # body gone (cache)
        from_load = [c for c in captured if c["url"] in covers]
        way1 = summary("capture responses while the page loads", from_load, round(time.time() - t1, 3), 0)

        # Way 2: fetch each image again from inside the page (same cookies, proxy and headers).
        t2 = time.time()
        again = page.evaluate(REFETCH, covers)
        refetched = [{"url": a["url"], "bytes": base64.b64decode(a["b64"])} for a in again]
        way2 = summary("fetch() each image again in the page", refetched, round(time.time() - t2, 3), len(covers))

        print(json.dumps([way1, way2], indent=2))
    finally:
        browser.close()
