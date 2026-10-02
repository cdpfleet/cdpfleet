# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# The live catalog says which channels and previous majors exist right now.
catalog = requests.get("https://cdpfleet.com/api/public/browsers", timeout=30).json()
versions = next(e for e in catalog["engines"] if e["key"] == "chrome")["versions"]
variants = []
for v in versions:
    if v["label"] == "pinned":
        major = v["version"].split(".")[0]
        variants.append((f"version {major}", {"version": major}, v["version"]))
    else:
        variants.append((f"channel {v['label']}", {} if v["label"] == "stable" else {"channel": v["label"]}, v["version"]))


def probe(variant):
    label, options, expected = variant
    # headless: False (a real display) so the user agent doesn't say "HeadlessChrome".
    res = requests.post("https://starter.cdpfleet.com/chrome/session", headers={"x-api-key": KEY},
                        json={"proxy": os.environ["PROXY_URL"], "headless": False, **options}, timeout=60)
    if not res.ok:
        return {"variant": label, "error": f"{res.status_code} {res.text}"}
    # The sync API is per thread: each probe gets its own Playwright instance.
    with sync_playwright() as p:
        browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            try:
                fp = browser.new_page().goto("https://tls.peet.ws/api/all", timeout=30000).json()
            except Exception:  # a residential exit occasionally times out: one retry, in a fresh tab
                fp = browser.new_page().goto("https://tls.peet.ws/api/all", timeout=30000).json()
            headers = next(f for f in fp["http2"]["sent_frames"] if f["frame_type"] == "HEADERS")["headers"]
            return {
                "variant": label,
                "catalog_version": expected,
                "browser_version": browser.version,
                "user_agent": fp["user_agent"],
                "sec_ch_ua": next((h[11:] for h in headers if h.startswith("sec-ch-ua: ")), None),
                "ja4": fp["tls"]["ja4"],
                "akamai_h2_hash": fp["http2"]["akamai_fingerprint_hash"],
            }
        finally:
            browser.close()


# All variants at once: each is its own session.
with ThreadPoolExecutor(len(variants)) as pool:
    print(json.dumps(list(pool.map(probe, variants)), indent=2))
