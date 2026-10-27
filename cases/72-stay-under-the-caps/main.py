# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from datetime import date

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
API = "https://cdpfleet.com"
# Twelve light, public catalogue pages: what a crawler works through.
URLS = [f"https://books.toscrape.com/catalogue/page-{i + 1}.html" for i in range(12)]
POOL = 4


def launch(p):
    res = requests.post("https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": "new", "record": False}, timeout=60)
    if not res.ok:
        raise SystemExit(f"launch {res.status_code} {res.text}")
    s = res.json()
    return s["sessionId"], p.chromium.connect(s["wsUrl"], headers={"x-api-key": KEY})


def end_reason(session_id):
    # How a session ended, from the account API (it appears a few seconds after the end).
    for _ in range(30):
        sessions = requests.get(f"{API}/v1/me/sessions?limit=50", headers={"x-api-key": KEY}, timeout=30).json().get("sessions", [])
        s = next((x for x in sessions if x["session_id"] == session_id), None)
        if s and s.get("ended_at"):
            return s["end_reason"]
        time.sleep(3)
    return None


def load(page, url):
    for attempt in (1, 2):
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=45000)
            return True
        except Exception:
            if attempt == 2:
                return False


sub = requests.get(f"{API}/v1/me", headers={"x-api-key": KEY}, timeout=30).json()["subscription"]
plan = {"kind": sub["kind"], "max_pages": sub["features"]["max_pages"], "memory_mb": sub["features"]["memory_mb"]}

with sync_playwright() as p:
    # 1 + 2: tabs. A new tab per URL, never closed, runs into the tab cap; a small pool of reused
    # tabs works through the same list without ever getting near it.
    _, browser = launch(p)
    try:
        ctx = browser.new_context()
        opened, closed_at = [], None
        for i in range(plan["max_pages"] + 1):
            # Over the cap the node closes the new tab at once: new_page() fails, or the tab is already closed.
            try:
                pg = ctx.new_page()
                time.sleep(1.5)
            except Exception:
                pg = None
            if pg is None or pg.is_closed():
                closed_at = i + 1
                break
            pg.set_content(f"<title>tab {i + 1}</title>")
            opened.append(pg)
        alive = browser.is_connected() and opened[0].title() == "tab 1"
        tab_cap = {"opened_before_cap": len(opened), "closed_at_tab": closed_at, "session_alive": alive}
        for pg in opened:
            pg.close()

        pages = [ctx.new_page() for _ in range(POOL)]
        loaded = 0
        # Sync Playwright loads one page at a time; the pool is still what keeps the tab count fixed.
        for i, url in enumerate(URLS):
            loaded += 1 if load(pages[i % POOL], url) else 0
        heaps = [pg.evaluate("() => (performance.memory && performance.memory.usedJSHeapSize) || 0") for pg in pages]
        heap_mb = round(sum(heaps) / len(heaps) / 1048576, 1)
        pool = {"pool_size": POOL, "urls": len(URLS), "loaded": loaded, "open_tabs": len(ctx.pages)}
        for pg in pages:
            pg.close()
    finally:
        browser.close()

    # 3: memory. Half the cap is fine; growing a page past the cap ends the session.
    session_id, browser = launch(p)
    mb, half_ok, cut = 0, False, False
    try:
        page = browser.new_page()
        page.evaluate("() => { window.hog = []; }")
        try:
            while mb < plan["memory_mb"] + 1024:
                page.evaluate("() => { const a = new Uint8Array(128 << 20); for (let i = 0; i < a.length; i += 4096) a[i] = 1; window.hog.push(a); }")
                mb += 128
                if mb >= plan["memory_mb"] / 2 and not half_ok:
                    half_ok = browser.is_connected()
        except Exception:
            cut = True
    finally:
        try:
            browser.close()
        except Exception:
            pass
    memory = {"half_cap_ok": half_ok, "cut": cut, "allocated_mb_at_cut": mb, "end_reason": end_reason(session_id)}

print(json.dumps({"measured_at": date.today().isoformat(), "plan": plan, "tab_cap": tab_cap, "pool": pool,
                  "js_heap_mb_per_tab": heap_mb, "memory": memory}, indent=2))
