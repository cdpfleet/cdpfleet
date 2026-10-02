# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL_US, PROXY_URL_DE, PROXY_URL_JP (exits in each country)
import json
import math
import os
from concurrent.futures import ThreadPoolExecutor

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
COUNTRIES = [
    {"country": "United States", "locale": "en-US", "proxy": os.environ["PROXY_URL_US"]},
    {"country": "Germany", "locale": "de-DE", "proxy": os.environ["PROXY_URL_DE"]},
    {"country": "Japan", "locale": "ja-JP", "proxy": os.environ["PROXY_URL_JP"]},
]

# What a localizing site reads in the page. Run in the PAGE's own JavaScript world
# ("mw:" prefix, needs main_world_eval): Playwright's default isolated world isn't patched
# the same way and can report the server's UTC timezone instead of the persona's.
LOCAL_VIEW = """(async () => {
  const position = await new Promise((ok) => navigator.geolocation.getCurrentPosition(
    (p) => ok({ lat: p.coords.latitude, lon: p.coords.longitude }), () => ok(null), { timeout: 10000 }));
  return {
    languages: navigator.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    date: new Date('2026-10-01T15:30:00Z').toLocaleString(),
    number: (1234567.891).toLocaleString(),
    price: new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(49.9),
    position,
  };
})()"""


def km(a, b):
    """Great-circle distance in km."""
    r = math.radians
    h = math.sin(r(b["lat"] - a["lat"]) / 2) ** 2 + math.cos(r(a["lat"])) * math.cos(r(b["lat"])) * math.sin(r(b["lon"] - a["lon"]) / 2) ** 2
    return round(12742 * math.asin(math.sqrt(h)))


def persona(c):
    # geoip: Camoufox sets timezone and geolocation from the proxy's exit IP at launch.
    res = requests.post("https://starter.cdpfleet.com/camoufox/session", headers={"x-api-key": KEY}, timeout=60,
                        json={"proxy": c["proxy"], "headless": True, "os": "windows", "locale": c["locale"],
                              "geoip": True, "main_world_eval": True})
    if not res.ok:
        return {"country": c["country"], "error": f"launch {res.status_code} {res.text}"}
    with sync_playwright() as p:  # the sync API is per thread
        browser = p.firefox.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        try:
            context = browser.new_context()
            context.grant_permissions(["geolocation"])  # as if the visitor clicked "Allow"
            page = context.new_page()
            exit_ = page.goto("http://ip-api.com/json/?fields=country,city,timezone,lat,lon", timeout=60000).json()
            page.goto("https://httpbin.org/html", timeout=60000)
            seen = page.evaluate(f"mw:{LOCAL_VIEW}")
            isolated = page.evaluate("Intl.DateTimeFormat().resolvedOptions().timeZone")
            return {"country": c["country"], "locale": c["locale"], "exit": f"{exit_['city']}, {exit_['country']}",
                    "exit_timezone": exit_["timezone"], **seen,
                    "position_km_from_exit": km(seen["position"], exit_) if seen["position"] else None,
                    "isolated_world_timezone": isolated}  # what a default page.evaluate would have reported
        finally:
            browser.close()


with ThreadPoolExecutor(len(COUNTRIES)) as pool:
    print(json.dumps(list(pool.map(persona, COUNTRIES)), indent=2, ensure_ascii=False))
