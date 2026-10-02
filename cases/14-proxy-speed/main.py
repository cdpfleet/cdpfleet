# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import json
import os
import statistics
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGE = "https://en.wikipedia.org/wiki/Web_browser"
LOADS = 3  # per proxy, each in a fresh context (cold cache, new connections)
PROXIES = {
    "residential (any country)": os.environ["PROXY_URL"],
    "residential (Germany)": os.environ["PROXY_URL_DE"],
    "datacenter SOCKS5": os.environ["SOCKS_PROXIES"].split(",")[0],
}

# Navigation Timing + Largest Contentful Paint, read in the page after load.
TIMINGS = """() => new Promise((done) => {
  const nav = performance.getEntriesByType('navigation')[0];
  new PerformanceObserver((list) => {
    const lcp = list.getEntries().at(-1);
    done({
      connect: nav.connectEnd - nav.connectStart,
      ttfb: nav.responseStart - nav.requestStart,
      domReady: nav.domContentLoadedEventEnd,
      load: nav.loadEventEnd,
      lcp: lcp.startTime,
    });
  }).observe({ type: 'largest-contentful-paint', buffered: true });
})"""


def measure(item):
    label, proxy = item
    res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                        json={"proxy": proxy, "headless": True}, timeout=60)
    if not res.ok:
        return {"proxy": label, "error": f"launch {res.status_code}"}
    with sync_playwright() as p:  # the sync API is per thread
        browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            runs, failures = [], 0
            while len(runs) < LOADS:
                context = browser.new_context()
                try:
                    page = context.new_page()
                    page.goto(PAGE, wait_until="load", timeout=60000)
                    runs.append(page.evaluate(TIMINGS))
                except Exception:
                    failures += 1
                    if failures > 1:  # one failed load is the proxy's noise; two is a problem
                        raise
                finally:
                    context.close()
            m = lambda k: round(statistics.median(r[k] for r in runs))
            return {"proxy": label, "loads": len(runs), "connect_ms": m("connect"), "ttfb_ms": m("ttfb"),
                    "dom_ready_ms": m("domReady"), "lcp_ms": m("lcp"), "load_ms": m("load")}
        finally:
            browser.close()


with ThreadPoolExecutor(len(PROXIES)) as pool:
    rows = list(pool.map(measure, PROXIES.items()))
print(json.dumps({"page": PAGE, "statistic": f"median of {LOADS} cold loads", "results": rows}, indent=2))
