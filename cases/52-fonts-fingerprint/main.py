# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import hashlib
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# The families a font-fingerprinting script typically tries: Windows, macOS, Linux and popular extras.
CANDIDATES = [
    "Arial", "Arial Black", "Calibri", "Cambria", "Candara", "Comic Sans MS", "Consolas", "Constantia", "Corbel", "Courier New",
    "Georgia", "Impact", "Lucida Console", "Lucida Sans Unicode", "Microsoft Sans Serif", "Palatino Linotype", "Segoe UI", "Tahoma",
    "Times New Roman", "Trebuchet MS", "Verdana", "American Typewriter", "Avenir", "Avenir Next", "Futura", "Geneva", "Gill Sans",
    "Helvetica", "Helvetica Neue", "Lucida Grande", "Menlo", "Monaco", "Optima", "SF Pro", "DejaVu Sans", "DejaVu Serif",
    "Liberation Sans", "Liberation Mono", "Noto Sans", "Ubuntu", "Cantarell", "Roboto", "Open Sans", "Fira Code", "JetBrains Mono",
    "Source Code Pro",
]

# The classic width probe: a family is "installed" if text set in it, with a generic fallback,
# measures differently from the generic family alone.
PROBE = """(fonts) => {
  const span = document.createElement('span');
  span.textContent = 'mmmmmmmmmmlli10OWQ@#';
  span.style.cssText = 'position:absolute;left:-9999px;font-size:72px';
  document.body.appendChild(span);
  const size = (family) => { span.style.fontFamily = family; return span.offsetWidth + 'x' + span.offsetHeight; };
  const generics = ['monospace', 'serif', 'sans-serif'];
  const base = {};
  for (const g of generics) base[g] = size(g);
  const found = fonts.filter((f) => generics.some((g) => size('"' + f + '", ' + g) !== base[g]));
  span.remove();
  return { platform: navigator.platform, found, generic_sizes: generics.map((g) => base[g]) };
}"""

SETUPS = [
    ("os: windows (launch 1)", {"os": "windows"}),
    ("os: windows (launch 2)", {"os": "windows"}),
    ("os: macos", {"os": "macos"}),
    ("os: linux", {"os": "linux"}),
    ("windows + fonts", {"os": "windows", "fonts": ["Menlo", "Helvetica Neue", "Fira Code"]}),
    ("windows + fonts + custom_fonts_only", {"os": "windows", "fonts": ["Calibri", "Consolas", "Segoe UI", "Fira Code"], "custom_fonts_only": True}),
]


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(body):
    for attempt in range(1, 6):
        r = requests.post("https://starter.cdpfleet.com/camoufox/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# Proxies drop a tunnel now and then: up to two retries.
def go(page, url):
    for attempt in range(1, 4):
        try:
            return page.goto(url, timeout=60000)
        except Exception:
            if attempt == 3:
                raise
            time.sleep(2)


results = []
with sync_playwright() as p:
    for name, body in SETUPS:
        session = launch({**body, "proxy": os.environ["PROXY_URL"], "headless": True, "record": False})
        browser = p.firefox.connect(session["wsUrl"], headers={"x-api-key": KEY})
        try:
            page = browser.new_context().new_page()
            go(page, "https://httpbin.org/html")
            r = page.evaluate(PROBE, CANDIDATES)
            results.append({
                "setup": name,
                "platform": r["platform"],
                "detected": len(r["found"]),
                "font_hash": hashlib.sha256(",".join(r["found"]).encode()).hexdigest()[:10],
                "generics_identical": len(set(r["generic_sizes"])) == 1,
                "fonts": r["found"],
            })
        finally:
            browser.close()
print(json.dumps(results, indent=2))
