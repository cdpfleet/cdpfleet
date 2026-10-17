# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
RATES = [1, 4, 6]
URL = "https://en.wikipedia.org/wiki/Web_browser"
# A fixed amount of JavaScript work, timed inside the page: what a heavy client-side app does.
WORK = """(() => {
  const t = performance.now();
  let h = 0;
  for (let i = 0; i < 3e6; i++) h = (h * 31 + i) % 1000003;
  const a = Array.from({ length: 200000 }, (_, i) => (i * 7919) % 100003).sort((x, y) => x - y);
  return { ms: Math.round(performance.now() - t), check: h + a[1000] };
})()"""
NAV = """() => {
  const n = performance.getEntriesByType('navigation')[0];
  return { response_ms: Math.round(n.responseEnd), dcl_ms: Math.round(n.domContentLoadedEventEnd), load_ms: Math.round(n.loadEventEnd) };
}"""


def metrics(cdp):
    return {m["name"]: m["value"] for m in cdp.send("Performance.getMetrics")["metrics"]}


res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = []
        for rate in RATES:
            context = browser.new_context()
            page = context.new_page()
            # CPU throttling is a DevTools feature: reach it with a CDP session over the Playwright connection.
            cdp = context.new_cdp_session(page)
            cdp.send("Performance.enable")
            cdp.send("Emulation.setCPUThrottlingRate", {"rate": rate})
            # A real page load: the network part isn't throttled, the main-thread work is.
            page.goto(URL, wait_until="load", timeout=90000)
            nav = page.evaluate(NAV)
            m = metrics(cdp)
            work = page.evaluate(WORK)
            rows.append({
                "rate": rate,
                "work_ms": work["ms"],
                **nav,
                "parse_ms": nav["dcl_ms"] - nav["response_ms"],  # HTML fully received → DOMContentLoaded: parsing and scripts
                "main_thread_ms": round(m["TaskDuration"] * 1000),  # busy time while loading, from CDP
                "check": work["check"],
            })
            context.close()

        # Is it per page? A throttled tab next to an unthrottled one in the same browser.
        context = browser.new_context()
        slow = context.new_page()
        fast = context.new_page()
        context.new_cdp_session(slow).send("Emulation.setCPUThrottlingRate", {"rate": 6})
        side_by_side = {"throttled_tab_ms": slow.evaluate(WORK)["ms"], "other_tab_ms": fast.evaluate(WORK)["ms"]}
        context.close()
        print(json.dumps({"rows": rows, "side_by_side": side_by_side}, indent=2))
    finally:
        browser.close()
