# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# Which browsers can print a page to PDF over a remote connection, headless and headful.
SETUPS = [
    {"label": "Chromium, headless", "engine": "chromium", "family": "chromium", "headless": True},
    {"label": "Chrome, headless", "engine": "chrome", "family": "chromium", "headless": "new"},
    {"label": "Chrome, headful", "engine": "chrome", "family": "chromium", "headless": False},
    {"label": "Edge, headful", "engine": "edge", "family": "chromium", "headless": False},
    {"label": "Firefox, headful", "engine": "firefox", "family": "firefox", "headless": False},
    {"label": "Camoufox, headful", "engine": "camoufox", "family": "firefox", "headless": False},
    {"label": "WebKit, headful", "engine": "webkit", "family": "webkit", "headless": False},
]

# A page with a coloured background, a screen-only banner hidden by print CSS and enough text
# for several printed pages. set_content: no proxy traffic, the same page everywhere.
PARAS = "".join(f"<p>Paragraph {i + 1}: lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>" for i in range(120))
HTML = f"""<html><head><style>
  body {{ font: 14px sans-serif; background: #e8f0ff; }}
  .screen-only {{ background: #c00; color: #fff; padding: 8px; }}
  @media print {{ .screen-only {{ display: none; }} }}
</style></head><body>
  <div class="screen-only">Cookie banner (screen only)</div>
  <h1>Quarterly report</h1>
  {PARAS}
</body></html>"""


def pages_in(pdf):
    return len(re.findall(rb"/Type\s*/Page(?!s)", pdf))


def launch(engine, body):
    # 503 means the fleet is momentarily busy: wait Retry-After and try again.
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=60)
        if res.ok:
            return res.json()
        if res.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {engine} {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after", 5)))


def one(p, s):
    session = launch(s["engine"], {"proxy": os.environ["PROXY_URL"], "headless": s["headless"]})
    browser = getattr(p, s["family"]).connect(session["wsUrl"], headers={"x-api-key": KEY})
    out = {"label": s["label"]}
    try:
        page = browser.new_page()
        page.set_content(HTML)
        t = time.time()
        try:
            pdf = page.pdf(format="A4")
            with_bg = page.pdf(format="A4", print_background=True)
            out.update(pdf="yes", pdf_bytes=len(pdf), pdf_pages=pages_in(pdf), with_background_bytes=len(with_bg),
                       pdf_seconds=round(time.time() - t, 2))
        except Exception as err:  # Firefox, Camoufox and WebKit refuse
            out.update(pdf="no", error=str(err).split("\n")[0][:120])
        # The fallback that works everywhere: print CSS + a full-page screenshot.
        page.emulate_media(media="print")
        out["print_css_applied"] = page.evaluate("() => getComputedStyle(document.querySelector('.screen-only')).display === 'none'")
        out["print_screenshot_kb"] = round(len(page.screenshot(full_page=True)) / 1024)
    finally:
        browser.close()
    return out


with sync_playwright() as p:
    results = [one(p, s) for s in SETUPS]
print(json.dumps({"results": results}, indent=2))
