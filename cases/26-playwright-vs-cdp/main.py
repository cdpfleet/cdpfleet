# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# What a page can notice about the client driving it. A debugger that has Runtime.enable'd
# the page serializes logged errors, which reads their `stack` getter.
PROBE = """(async () => {
  let stackRead = false;
  const e = new Error('probe');
  Object.defineProperty(e, 'stack', { get() { stackRead = true; return ''; } });
  console.debug(e);
  await new Promise((r) => setTimeout(r, 100));
  return { stack_read_by_debugger: stackRead, webdriver: navigator.webdriver };
})()"""


def launch(cdp):
    res = requests.post("https://starter.cdpfleet.com/chrome/session", headers={"x-api-key": KEY}, timeout=60,
                        json={"proxy": os.environ["PROXY_URL"], "headless": "new", "cdp": cdp})
    if not res.ok:
        raise SystemExit(f"launch {res.status_code} {res.text}")
    return res.json()


def measure(p, mode):
    s = launch(mode != "playwright protocol")
    t = time.time()
    if mode == "playwright protocol":
        browser = p.chromium.connect(s["wsUrl"], headers={"x-api-key": KEY})
    else:
        browser = p.chromium.connect_over_cdp(s["cdpUrl"], headers={"x-api-key": KEY})
    connect_ms = round((time.time() - t) * 1000)
    try:
        page = browser.new_page()
        logged = []
        page.on("console", lambda m: logged.append(m.type))
        for attempt in range(1, 4):  # the proxy can drop a tunnel; retry
            try:
                page.goto("https://example.com/", timeout=60000)
                break
            except Exception:
                if attempt == 3:
                    raise
        seen = page.evaluate(PROBE)
        page.wait_for_timeout(200)  # let the console event arrive
        try:
            pdf = len(page.pdf()) > 0
        except Exception:  # not over this connection
            pdf = False
        return {
            "mode": mode,
            "endpoint": "wsUrl" if mode == "playwright protocol" else "cdpUrl",
            "client_version_must_match": mode == "playwright protocol",
            "connect_ms": connect_ms,
            "browser_version": browser.version,
            "console_events": "debug" in logged,
            "pdf": pdf,
            **seen,
        }
    finally:
        browser.close()


with sync_playwright() as p:
    print(json.dumps([measure(p, "playwright protocol"), measure(p, "connectOverCDP")], indent=2))
