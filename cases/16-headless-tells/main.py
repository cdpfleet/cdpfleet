# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]

# Four ways to run a browser; the same twelve signals read from inside the page.
TARGETS = [
    {"label": "Chromium, headless", "engine": "chromium", "family": "chromium", "body": {"headless": "new"}},
    {"label": "Chromium, headful", "engine": "chromium", "family": "chromium", "body": {"headless": False}},
    {"label": "Patchright, headful", "engine": "patchright", "family": "chromium", "body": {"headless": False}},
    # Camoufox: read the page's own world ("mw:" needs main_world_eval), like a site would.
    {"label": "Camoufox, headful", "engine": "camoufox", "family": "firefox",
     "body": {"headless": False, "os": "windows", "main_world_eval": True}, "prefix": "mw:"},
]

# The classic headless and automation tells, read the way a detection script reads them.
SIGNALS = """(async () => {
  let query = null;
  try { query = (await navigator.permissions.query({ name: 'notifications' })).state; } catch { query = 'error'; }
  const gl = (() => {
    try { const g = document.createElement('canvas').getContext('webgl'); const d = g.getExtension('WEBGL_debug_renderer_info'); return g.getParameter(d ? d.UNMASKED_RENDERER_WEBGL : g.RENDERER); } catch { return null; }
  })();
  return {
    ua_says_headless: /Headless/.test(navigator.userAgent),
    webdriver: navigator.webdriver,
    plugins: navigator.plugins.length,
    notification_mismatch: typeof Notification !== 'undefined' && Notification.permission === 'denied' && query === 'prompt',
    outer_window_zero: outerWidth === 0 || outerHeight === 0,
    screen: screen.width + 'x' + screen.height,
    webgl_renderer: gl,
    cores: navigator.hardwareConcurrency,
    memory_gb: navigator.deviceMemory ?? null,
    languages: navigator.languages.join(','),
    chrome_object: typeof window.chrome === 'object' && window.chrome !== null,
  };
})()"""

TELLS = ["ua_says_headless", "webdriver", "notification_mismatch", "outer_window_zero"]


def inspect(target):
    res = requests.post(f"https://starter.cdpfleet.com/{target['engine']}/session", headers={"x-api-key": KEY},
                        json={"proxy": PROXY, **target["body"]}, timeout=60)
    if not res.ok:
        return {"label": target["label"], "error": f"launch {res.status_code} {res.text}"}
    session = res.json()
    with sync_playwright() as p:  # the sync API is per thread
        browser = getattr(p, target["family"]).connect(session["wsUrl"], headers={"x-api-key": KEY})
        try:
            page = browser.new_page()
            page.goto("https://example.com/", timeout=60000)
            signals = page.evaluate(target.get("prefix", "") + SIGNALS)
            tells = [k for k in TELLS if signals[k]]
            return {"label": target["label"], "threads": session["weight"], **signals,
                    "tells": ", ".join(tells) if tells else "none"}
        finally:
            browser.close()


with ThreadPoolExecutor(len(TARGETS)) as pool:
    print(json.dumps(list(pool.map(inspect, TARGETS)), indent=2, ensure_ascii=False))
