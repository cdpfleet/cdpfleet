# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (no proxy of your own needed)
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]


def base36(n):
    digits = "0123456789abcdefghijklmnopqrstuvwxyz"
    out = ""
    while n:
        n, r = divmod(n, 36)
        out = digits[r] + out
    return out or "0"


SESSION = "c25" + base36(int(time.time() * 1000))[-6:]  # a sticky name, 1–10 of [A-Za-z0-9_]

# Four launches, all on the cdpfleet residential proxy — the token is the whole proxy config.
RUNS = [
    {"label": "rotating, any country", "proxy": "cdpfleet-resi"},
    {"label": "rotating, Germany", "proxy": "cdpfleet-resi-country-de"},
    {"label": "sticky US, browser 1", "proxy": f"cdpfleet-resi-country-us-session-{SESSION}-lifetime-10"},
    {"label": "sticky US, browser 2", "proxy": f"cdpfleet-resi-country-us-session-{SESSION}-lifetime-10"},
]


def lookup(page, i):
    # Residential peers drop a few percent of connections: retry a lookup up to 3 times.
    for attempt in range(1, 4):
        try:
            return page.goto(f"http://ip-api.com/json/?fields=query,countryCode&i={i}-{attempt}", timeout=60000).json()
        except Exception:
            if attempt == 3:
                raise


def run(p, label, proxy):
    res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, timeout=60,
                        json={"proxy": proxy, "headless": "new"})
    if not res.ok:
        return {"label": label, "error": f"launch {res.status_code} {res.text}"}
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        # Three lookups on separate connections: rotating exits change, sticky ones don't.
        exits = [lookup(page, i) for i in range(3)]
        countries = []
        for e in exits:
            if e["countryCode"] not in countries:
                countries.append(e["countryCode"])
        return {
            "label": label,
            "proxy": proxy.replace(SESSION, "<name>"),
            "countries": ",".join(countries),
            "distinct_ips": len({e["query"] for e in exits}),
            "first_ip": exits[0]["query"],
        }
    finally:
        browser.close()


with sync_playwright() as p:
    out = [run(p, r["label"], r["proxy"]) for r in RUNS]  # in order, so browser 2 starts after browser 1 ended

b1, b2 = out[2], out[3]
for r in out:
    r["same_ip_as_browser_1"] = r["first_ip"] == b1["first_ip"] if r["label"].startswith("sticky") else None
# The balance the traffic is billed to (metered about once a minute, so it trails a little).
bal = requests.get("https://cdpfleet.com/v1/me/proxy-balance", headers={"x-api-key": KEY}, timeout=30).json()
for r in out:
    del r["first_ip"]
print(json.dumps({
    "runs": out,
    "sticky_ip_kept_across_browsers": b2["same_ip_as_browser_1"],
    "balance_allowed": bal["allowed"],
    "price_per_gb_usd": bal["price_per_gb_usd"],
}, indent=2))
