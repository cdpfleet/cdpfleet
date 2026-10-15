# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
WAIT_SECONDS = 120  # between the two snapshots — in a real watcher, your scheduler's interval

# What to watch: a catalogue page (its price only changes when the shop changes it) and a live
# market price that moves by the second.
TARGETS = [
    {"name": "Book price", "url": "https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html",
     "read": """() => ({ price: document.querySelector('.product_main .price_color').textContent.trim(), stock: document.querySelector('.product_main .availability').textContent.trim().replace(/\\s+/g, ' ') })"""},
    {"name": "Bitcoin spot (USD)", "url": "https://api.coinbase.com/v2/prices/BTC-USD/spot",
     "read": "() => ({ price: JSON.parse(document.body.innerText).data.amount })"},
]


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(body):
    for attempt in range(1, 6):
        r = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# One snapshot = one short session: launch, read every target, close. Nothing is billed while
# waiting, and an idle session would end after 60 s anyway.
def snapshot(pw):
    t = time.time()
    s = launch({"proxy": os.environ["PROXY_URL"], "headless": True, "record": False})
    browser = pw.chromium.connect(s["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        values = {}
        for target in TARGETS:
            for attempt in range(1, 4):  # proxies drop a tunnel now and then
                try:
                    page.goto(target["url"], timeout=45000)
                    values[target["name"]] = page.evaluate(target["read"])
                    break
                except Exception:
                    if attempt == 3:
                        raise
        return {"at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"), "seconds": round(time.time() - t, 1), "values": values}
    finally:
        browser.close()


def number(v):
    try:
        return float(re.sub(r"[^0-9.]", "", str(v)))
    except ValueError:
        return 0


# Field-by-field diff of two snapshots, with the percentage for numeric prices.
def diff(a, b):
    out = []
    for target in TARGETS:
        name = target["name"]
        changes = []
        for k in a["values"][name]:
            x, y = a["values"][name][k], b["values"][name][k]
            if x != y:
                c = {"field": k, "before": x, "after": y}
                nx, ny = number(x), number(y)
                if nx and ny:
                    c["percent"] = round((ny - nx) / nx * 100, 2)
                changes.append(c)
        out.append({"target": name, "changed": bool(changes), "changes": changes, "now": b["values"][name]})
    return out


with sync_playwright() as pw:
    first = snapshot(pw)
    time.sleep(WAIT_SECONDS)
    second = snapshot(pw)
print(json.dumps({
    "wait_seconds": WAIT_SECONDS,
    "billed_seconds": round(first["seconds"] + second["seconds"], 1),
    "first_at": first["at"], "second_at": second["at"],
    "diff": diff(first, second),
}, indent=2, ensure_ascii=False))
