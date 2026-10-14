# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SETUPS = [
    {"label": "Chrome", "engine": "chrome", "family": "chromium", "api": "typeof chrome !== 'undefined' && !!chrome.runtime"},
    {"label": "Firefox", "engine": "firefox", "family": "firefox", "api": "typeof browser !== 'undefined' && !!browser.runtime"},
]
SITE = "https://books.toscrape.com/"

# What an extension's content script does: runs in every page before the page's own scripts.
CONTENT_SCRIPT = """
  window.__myExtension = true;
  document.addEventListener('DOMContentLoaded', () => {
    const b = document.createElement('div');
    b.id = 'my-extension-banner';
    b.textContent = 'Injected on ' + location.pathname;
    document.body.prepend(b);
  });"""


def launch(engine, body):
    # 503 means the fleet is momentarily busy: wait Retry-After and try again.
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=60)
        if res.ok:
            return res.status_code, res.json()
        if res.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {engine} {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after", 5)))


def one(p, s):
    # Try to load an extension the way a local launch would: Chromium flags and an "extensions" list.
    status, session = launch(s["engine"], {
        "proxy": os.environ["PROXY_URL"],
        "args": ["--load-extension=/tmp/my-extension", "--user-agent=ARGS-WERE-APPLIED"],
        "extensions": ["/tmp/my-extension"],
    })
    browser = getattr(p, s["family"]).connect(session["wsUrl"], headers={"x-api-key": KEY})
    out = {"label": s["label"], "launch_status": status}
    try:
        page = browser.new_page()
        out["args_applied"] = "ARGS-WERE-APPLIED" in page.evaluate("navigator.userAgent")
        # chrome.runtime / browser.runtime exist in a page only when an extension talks to it.
        out["extension_api"] = page.evaluate(s["api"])
        page.close()

        # The Playwright equivalents, in one context.
        context = browser.new_context()
        context.add_init_script(CONTENT_SCRIPT)  # content script
        context.set_extra_http_headers({"x-my-extension": "on"})  # header rewriting
        blocked = [0]  # request blocking, like an ad or image blocker

        def block(route):
            blocked[0] += 1
            route.abort()

        context.route(re.compile(r"\.(png|jpe?g|gif|webp)(\?|$)"), block)
        pg = context.new_page()
        injected = 0
        for url in [SITE, f"{SITE}catalogue/page-2.html"]:
            pg.goto(url, timeout=60000)
            if pg.locator("#my-extension-banner").count():
                injected += 1
        out["content_script_pages"] = injected
        out["blocked_images"] = blocked[0]
        # httpbin echoes the request headers; read the response body, not the rendered page.
        headers = pg.goto("https://httpbin.org/headers", timeout=60000).json()["headers"]
        out["header_added"] = headers.get("X-My-Extension") == "on"
    finally:
        browser.close()
    return out


with sync_playwright() as p:
    results = [one(p, s) for s in SETUPS]
print(json.dumps({"results": results}, indent=2))
