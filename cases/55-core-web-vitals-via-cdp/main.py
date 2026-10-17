# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PAGES = [
    ("example.com", "https://example.com/"),
    ("books.toscrape.com", "https://books.toscrape.com/"),
    ("Wikipedia article", "https://en.wikipedia.org/wiki/Web_browser"),
    ("shifting demo", "https://shift.test/"),  # served by page.route below
]
# A made-up page whose banner arrives late and pushes the text down: a layout shift on purpose.
SHIFTING = ("""<!doctype html><meta charset="utf-8"><title>shift</title>
<body style="margin:0;font:18px sans-serif"><div id="slot"></div>
<h1>Article</h1>""" + "<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>" * 30 + """
<script>setTimeout(() => { document.getElementById('slot').innerHTML = '<div style="height:300px;background:#fc0">Late banner</div>'; }, 600);</script>""")

# Reads the vitals from the page's own performance timeline. `buffered: true` returns the
# entries recorded before the observer existed, so this runs after the load.
VITALS = """new Promise((resolve) => {
  const nav = performance.getEntriesByType('navigation')[0];
  const fcp = performance.getEntriesByName('first-contentful-paint')[0];
  let lcp = null, cls = 0;
  new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e; }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  setTimeout(() => resolve({
    ttfb_ms: Math.round(nav.responseStart),
    connect_ms: Math.round(nav.connectEnd - nav.domainLookupStart),
    wait_ms: Math.round(nav.responseStart - nav.requestStart),
    fcp_ms: fcp ? Math.round(fcp.startTime) : null,
    lcp_ms: lcp ? Math.round(lcp.startTime) : null,
    lcp_element: lcp && lcp.element ? lcp.element.tagName.toLowerCase() : null,
    cls: Math.round(cls * 1000) / 1000,
    load_ms: Math.round(nav.loadEventEnd),
  }), 200);
})"""

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = []
        for name, url in PAGES:
            context = browser.new_context(viewport={"width": 1280, "height": 800})  # cold cache
            context.route("https://shift.test/**", lambda route: route.fulfill(content_type="text/html; charset=utf-8", body=SHIFTING))
            page = context.new_page()
            # A CDP session over the normal Playwright connection: the browser's own counters.
            cdp = context.new_cdp_session(page)
            cdp.send("Performance.enable")
            page.goto(url, wait_until="load", timeout=60000)
            page.wait_for_timeout(1500)  # let late content (and late shifts) land
            vitals = page.evaluate(VITALS)
            metrics = {m["name"]: m["value"] for m in cdp.send("Performance.getMetrics")["metrics"]}
            rows.append({
                "page": name, **vitals,
                "script_ms": round(metrics["ScriptDuration"] * 1000),
                "layouts": int(metrics["LayoutCount"]),
                "dom_nodes": int(metrics["Nodes"]),
                "js_heap_mb": round(metrics["JSHeapUsedSize"] / 1048576, 1),
            })
            context.close()
        print(json.dumps({"rows": rows}, indent=2))
    finally:
        browser.close()
