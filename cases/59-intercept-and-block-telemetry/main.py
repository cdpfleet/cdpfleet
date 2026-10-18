# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_US
import json
import os
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITES = [
    ("The Guardian", "https://www.theguardian.com/international"),
    ("NPR", "https://www.npr.org/"),
    ("Wikipedia", "https://en.wikipedia.org/wiki/Web_browser"),
]
# Analytics, measurement and real-user-monitoring hosts (subdomains included) — not ads.
TELEMETRY = ["google-analytics.com", "analytics.google.com", "googletagmanager.com", "ophan.theguardian.com",
             "performance-events.guardianapis.com", "feast-events.guardianapis.com", "scorecardresearch.com", "permutive.app",
             "permutive.com", "go-mpulse.net", "akstat.io", "nr-data.net", "newrelic.com", "sentry.io", "datadoghq.com",
             "hotjar.com", "clarity.ms", "segment.io", "segment.com", "chartbeat.com", "chartbeat.net", "parsely.com",
             "analytics.yahoo.com", "omtrdc.net", "demdex.net", "bat.bing.com", "cloudflareinsights.com", "brandmetrics.com"]


def is_telemetry(req):
    """Telemetry: one of those hosts, or a beacon (navigator.sendBeacon, <a ping>)."""
    if req.resource_type == "ping":
        return True
    host = urlparse(req.url).hostname or ""
    return any(host == d or host.endswith("." + d) for d in TELEMETRY)


res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL_US"], "headless": True}, timeout=60)
res.raise_for_status()


def load(browser, url, block):
    """One load in a fresh context: DOM ready + 6 s, then leave the page (about:blank) and give the
    page-hide beacons 1.5 s. Counts every request, the telemetry among them, and bytes on the wire."""
    context = browser.new_context()
    m = {"requests": 0, "telemetry": 0, "on_leave": 0, "bytes": 0, "telemetry_bytes": 0, "blocked": 0, "leaving": False}
    if block:
        def handle(route):
            if is_telemetry(route.request):
                m["blocked"] += 1
                return route.abort()
            return route.continue_()
        context.route("**/*", handle)
    page = context.new_page()

    def on_request(req):
        m["requests"] += 1
        if is_telemetry(req):
            m["telemetry"] += 1
            if m["leaving"]:
                m["on_leave"] += 1

    def on_finished(req):
        try:
            s = req.sizes()
        except Exception:
            return
        n = s["requestHeadersSize"] + s["requestBodySize"] + s["responseHeadersSize"] + s["responseBodySize"]
        m["bytes"] += n
        if is_telemetry(req):
            m["telemetry_bytes"] += n

    page.on("request", on_request)
    page.on("requestfinished", on_finished)
    t = time.time()
    page.goto(url, wait_until="domcontentloaded", timeout=90000)
    dom_ready = round((time.time() - t) * 1000)
    page.wait_for_timeout(6000)
    title = page.title()
    headings = page.locator("h1, h2, h3").count()  # the content is still there
    m["leaving"] = True
    page.goto("about:blank")
    page.wait_for_timeout(1500)
    context.close()
    return {**m, "dom_ready": dom_ready, "title": title, "headings": headings}


with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        rows = []
        for name, url in SITES:
            off = load(browser, url, False)
            on = load(browser, url, True)
            rows.append({
                "site": name,
                "title_ok": bool(off["title"]) and off["title"] == on["title"],
                "requests": off["requests"],
                "telemetry_requests": off["telemetry"],
                "telemetry_on_leave": off["on_leave"],
                "telemetry_kb": round(off["telemetry_bytes"] / 1024),
                "total_kb": round(off["bytes"] / 1024),
                "blocked": on["blocked"],
                "headings": off["headings"],
                "headings_blocked_run": on["headings"],
                "dom_ready_ms": off["dom_ready"],
                "dom_ready_ms_blocked_run": on["dom_ready"],
            })
        print(json.dumps(rows, indent=2, ensure_ascii=False))
    finally:
        browser.close()
