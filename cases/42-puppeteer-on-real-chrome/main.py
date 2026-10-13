# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
# Python has no maintained Puppeteer, so this is Playwright over CDP: connect_over_cdp(cdpUrl)
# plus raw CDP sessions for the same browser- and page-level commands Puppeteer sends.
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
URL = "https://books.toscrape.com/"

# Bytes the page pulled over the network, from its own resource timing (fresh context: no cache).
TRANSFER = """() => {
  const all = [...performance.getEntriesByType('navigation'), ...performance.getEntriesByType('resource')];
  return { resources: all.length, transfer_kb: Math.round(all.reduce((n, e) => n + (e.transferSize || 0), 0) / 1024) };
}"""


def launch():
    res = requests.post(
        "https://starter.cdpfleet.com/chrome/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": "new", "cdp": True, "record": False},
        timeout=60,
    )
    res.raise_for_status()
    return res.json()


def is_live(session_id):
    return session_id in requests.get("https://cdpfleet.com/v1/me/threads", headers={"x-api-key": KEY}, timeout=30).text


def goto(page):
    for attempt in range(1, 4):  # the proxy can drop a tunnel; retry
        try:
            page.goto(URL, wait_until="load", timeout=60000)
            return
        except Exception:
            if attempt == 3:
                raise


def load(browser, block_images):
    """One page in a fresh browser context, optionally aborting image requests."""
    context = browser.new_context()
    page = context.new_page()
    blocked = 0
    if block_images:
        def route(r):
            nonlocal blocked
            if r.request.resource_type == "image":
                blocked += 1
                r.abort()
            else:
                r.continue_()
        page.route("**/*", route)
    t = time.time()
    goto(page)
    load_ms = round((time.time() - t) * 1000)
    transfer = page.evaluate(TRANSFER)
    row = {"label": "images blocked" if block_images else "everything", **transfer, "images_blocked": blocked, "load_ms": load_ms}
    return context, page, row


s = launch()
out = {"client": "Playwright 1.60 connect_over_cdp"}
with sync_playwright() as p:
    t = time.time()
    browser = p.chromium.connect_over_cdp(s["cdpUrl"], headers={"x-api-key": KEY})
    out["connect_ms"] = round((time.time() - t) * 1000)
    try:
        # Raw CDP at the browser level: what's really on the other end.
        cdp = browser.new_browser_cdp_session()
        version = cdp.send("Browser.getVersion")
        out["product"] = version["product"]
        out["protocol_version"] = version["protocolVersion"]

        def pages():
            return len([x for x in cdp.send("Target.getTargets")["targetInfos"] if x["type"] == "page"])

        before = pages()
        full_ctx, full_page, full_row = load(browser, False)
        lean_ctx, _, lean_row = load(browser, True)
        out["targets"] = {"pages_before": before, "pages_after": pages()}
        out["loads"] = [full_row, lean_row]

        # Performance.getMetrics over a page-level CDP session (Puppeteer's page.metrics()).
        page_cdp = full_ctx.new_cdp_session(full_page)
        page_cdp.send("Performance.enable")
        m = {x["name"]: x["value"] for x in page_cdp.send("Performance.getMetrics")["metrics"]}
        out["metrics"] = {"dom_nodes": int(m["Nodes"]), "js_heap_used_mb": round(m["JSHeapUsedSize"] / 1048576, 1)}
        full_ctx.close()
        lean_ctx.close()
    finally:
        browser.close()  # closes the connection, which ends the cdpfleet session

closed = time.time()
while is_live(s["sessionId"]) and time.time() - closed < 60:
    time.sleep(1)
out["session_ended_s"] = None if is_live(s["sessionId"]) else round(time.time() - closed, 1)
print(json.dumps(out, indent=2))
