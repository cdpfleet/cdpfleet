# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# Content a plain document.querySelector can't see: a same-origin iframe, a cross-origin
# iframe, an open shadow root (with another one nested inside) and a closed shadow root.
HTML = """<!doctype html><title>Hidden content</title>
<h1>Main document</h1>
<iframe id="same" srcdoc="<p id='inner'>same-origin iframe text</p>"></iframe>
<iframe id="cross" src="https://httpbin.org/html"></iframe>
<open-card></open-card>
<closed-card></closed-card>
<script>
customElements.define('open-card', class extends HTMLElement {
  connectedCallback() {
    const root = this.attachShadow({ mode: 'open' });
    root.innerHTML = '<p class="msg">open shadow text</p><nested-badge></nested-badge>';
  }
});
customElements.define('nested-badge', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<span class="badge">nested shadow text</span>'; }
});
customElements.define('closed-card', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'closed' }).innerHTML = '<p class="secret">closed shadow text</p>'; }
});
</script>"""

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        page.set_content(HTML)
        page.frame_locator("#cross").locator("h1").wait_for(timeout=60000)

        def qs(sel):
            return page.evaluate("(s) => document.querySelector(s)?.textContent ?? null", sel)

        def pw(loc):
            return loc.first.text_content() if loc.count() else None

        rows = [
            {"target": "same-origin iframe", "querySelector": qs("#inner"), "playwright": pw(page.frame_locator("#same").locator("#inner")), "how": "page.frameLocator('#same').locator('#inner')"},
            {"target": "cross-origin iframe", "querySelector": qs("h1 + div p"), "playwright": pw(page.frame_locator("#cross").locator("h1")), "how": "page.frameLocator('#cross').locator('h1')"},
            {"target": "open shadow root", "querySelector": qs(".msg"), "playwright": pw(page.locator(".msg")), "how": "page.locator('.msg') — CSS pierces open shadow roots"},
            {"target": "nested open shadow root", "querySelector": qs(".badge"), "playwright": pw(page.locator(".badge")), "how": "page.locator('.badge') — any depth"},
            {"target": "closed shadow root", "querySelector": qs(".secret"), "playwright": pw(page.locator(".secret")), "how": "not reachable from page scripts or locators"},
        ]
        # The cross-origin frame is a separate document: its URL and title come from the frame object.
        cross = next((f for f in page.frames if f.url.startswith("https://httpbin.org")), None)
        print(json.dumps({
            "frames": len(page.frames),
            "cross_origin_frame": {"url": cross.url if cross else None, "title": cross.title() if cross else None},
            "rows": rows,
        }, indent=2, ensure_ascii=False))
    finally:
        browser.close()
