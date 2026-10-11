# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import shutil
import tempfile
import time
import zipfile

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URL = "https://books.toscrape.com/"

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    tmp = tempfile.mkdtemp(prefix="har-")
    har = os.path.join(tmp, "books.har")
    # Over connect() the Python client only delivers a .har as a .zip archive (a plain .har
    # path is left as an unextracted "books.har.tmp"), so record into a zip and unpack it.
    har_zip = os.path.join(tmp, "books.zip")
    try:
        # 1. Record: every request of the page goes into a HAR file on this machine.
        rec = browser.new_context(record_har_path=har_zip, record_har_mode="minimal",
                                  record_har_content="embed")
        p1 = rec.new_page()
        t = time.time()
        p1.goto(URL, wait_until="load", timeout=60000)
        live_ms = round((time.time() - t) * 1000)
        live_books = p1.locator("article.product_pod").count()
        rec.close()  # writes the HAR
        with zipfile.ZipFile(har_zip) as z, open(har, "wb") as f:
            f.write(z.read(next(n for n in z.namelist() if n.endswith(".har"))))
        with open(har, encoding="utf-8") as f:
            entries = json.load(f)["log"]["entries"]

        # 2. Replay: serve every request from the HAR; anything not in it is refused, so the
        #    page can't quietly reach the network.
        rep = browser.new_context()
        counts = {"from_har": 0, "refused": 0}
        rep.on("requestfinished", lambda _: counts.__setitem__("from_har", counts["from_har"] + 1))
        rep.on("requestfailed", lambda _: counts.__setitem__("refused", counts["refused"] + 1))
        rep.route_from_har(har, not_found="abort")
        p2 = rep.new_page()
        t = time.time()
        p2.goto(URL, wait_until="load", timeout=60000)
        replay_ms = round((time.time() - t) * 1000)
        replay_books = p2.locator("article.product_pod").count()
        # A page the HAR doesn't have: refused instead of fetched.
        try:
            p2.goto("https://books.toscrape.com/catalogue/page-2.html")
            missing = "loaded"
        except Exception as e:
            missing = "refused" if re.search(r"ERR_FAILED|aborted", str(e), re.I) else str(e).split("\n")[0]
        rep.close()

        print(json.dumps({
            "har_entries": len(entries),
            "har_kb": round(os.path.getsize(har) / 1024),
            "live": {"ms": live_ms, "books": live_books},
            "replay": {"ms": replay_ms, "books": replay_books,
                       "served_from_har": counts["from_har"], "refused": counts["refused"]},
            "page_not_in_har": missing,
        }, indent=2))
    finally:
        browser.close()
        shutil.rmtree(tmp, ignore_errors=True)
