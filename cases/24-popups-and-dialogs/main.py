# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# A page with everything that interrupts a script: a new-tab link, window.open, and the
# three blocking dialogs. Served from the browser itself, so the case needs no third party.
HTML = """<!doctype html><title>Interruptions</title>
<a id="blank" href="https://example.com/" target="_blank">open in a new tab</a>
<button id="open" onclick="window.open('https://example.com/?popup', 'pop', 'width=480,height=320')">window.open</button>
<button id="alert" onclick="alert('Saved!')">alert</button>
<button id="confirm" onclick="document.body.dataset.confirm = String(confirm('Delete 3 items?'))">confirm</button>
<button id="prompt" onclick="document.body.dataset.prompt = String(prompt('Your name?', 'anonymous'))">prompt</button>"""

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, timeout=60,
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"})
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")
session = res.json()

with sync_playwright() as p:
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        context = browser.new_context()
        page = context.new_page()
        page.set_content(HTML)
        out = []

        # New pages: listen on the context BEFORE the click, then wait for the popup to load.
        for label, selector in [("link with target=_blank", "#blank"), ("window.open()", "#open")]:
            with context.expect_page() as popup_info:
                page.click(selector)
            popup = popup_info.value
            popup.wait_for_load_state("load", timeout=60000)
            out.append({"event": label, "what_happened": f'new page: {popup.url} — "{popup.title()}"',
                        "handled_with": 'context.waitForEvent("page") + popup.waitForLoadState()',
                        "pages_open": len(context.pages), "opener_is_main_page": popup.opener() is page})
            popup.close()

        # Dialogs: without a handler Playwright dismisses them (confirm → false, prompt → null).
        page.click("#confirm")
        out.append({"event": "confirm() with no dialog handler",
                    "what_happened": f"page saw confirm() return {page.evaluate('() => document.body.dataset.confirm')}",
                    "handled_with": "nothing — auto-dismissed", "pages_open": len(context.pages), "opener_is_main_page": None})

        # With a handler you decide: accept, dismiss, or type an answer.
        seen = []

        def on_dialog(d):
            seen.append(f'{d.type}: "{d.message}"')
            if d.type == "prompt":
                d.accept("Ada Lovelace")
            else:
                d.accept()

        page.on("dialog", on_dialog)
        page.click("#alert")
        page.click("#confirm")
        page.click("#prompt")
        results = page.evaluate("() => ({ confirm: document.body.dataset.confirm, prompt: document.body.dataset.prompt })")
        out.append({"event": "alert()", "what_happened": seen[0], "handled_with": "dialog.accept()",
                    "pages_open": len(context.pages), "opener_is_main_page": None})
        out.append({"event": "confirm() with a handler", "what_happened": f"{seen[1]} → page saw {results['confirm']}",
                    "handled_with": "dialog.accept()", "pages_open": len(context.pages), "opener_is_main_page": None})
        out.append({"event": "prompt()", "what_happened": f"{seen[2]} → page saw \"{results['prompt']}\"",
                    "handled_with": 'dialog.accept("Ada Lovelace")', "pages_open": len(context.pages), "opener_is_main_page": None})

        print(json.dumps(out, indent=2, ensure_ascii=False))
    finally:
        browser.close()
