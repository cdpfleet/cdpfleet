# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy)
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
BERLIN = {"latitude": 52.52, "longitude": 13.405}

# What a page can learn about where the visitor is, from JavaScript.
PROBE = """async () => {
  const geo = await new Promise((resolve) => navigator.geolocation.getCurrentPosition(
    (p) => resolve({ lat: Math.round(p.coords.latitude * 100) / 100, lon: Math.round(p.coords.longitude * 100) / 100 }),
    (e) => resolve({ error: e.code === 1 ? 'permission denied' : e.message }),
    { timeout: 5000 }));
  const perm = (await navigator.permissions.query({ name: 'geolocation' })).state;
  return { geolocation: geo, permission: perm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, language: navigator.language, utc_offset_min: -new Date().getTimezoneOffset() };
}"""

res = requests.post("https://starter.cdpfleet.com/chromium/session",
    headers={"x-api-key": KEY},
    json={"proxy": os.environ["PROXY_URL_DE"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        out = {"ip": None, "contexts": []}
        setups = [
            ("defaults", {}),
            ("permission granted, no position", {"permissions": ["geolocation"]}),
            ("Berlin: geolocation + timezone + locale", {"permissions": ["geolocation"], "geolocation": BERLIN, "timezone_id": "Europe/Berlin", "locale": "de-DE"}),
        ]
        for label, options in setups:
            ctx = browser.new_context(**options)
            page = ctx.new_page()
            if out["ip"] is None:
                page.goto("http://ip-api.com/json/?fields=query,countryCode,timezone", timeout=60000)
                ip = json.loads(page.text_content("body"))
                out["ip"] = {"country": ip["countryCode"], "timezone": ip["timezone"]}
            page.goto("https://httpbin.org/html", timeout=60000)
            out["contexts"].append({"setup": label, **page.evaluate(f"({PROBE})()")})
            ctx.close()
        print(json.dumps(out, indent=2, ensure_ascii=False))
    finally:
        browser.close()
