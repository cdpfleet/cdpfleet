# pip install playwright==1.60.0 requests
# env: COMMUNITY_API_KEY
import json
import os
import random
import re
import string
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["COMMUNITY_API_KEY"]


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(body):
    for attempt in range(1, 6):
        r = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# The exit the site sees. Shared proxies drop the odd connection: retry a few times.
def exit_of(page):
    for i in range(1, 5):
        try:
            page.goto("http://ip-api.com/json", timeout=30000)
            j = json.loads(page.text_content("body"))
            return {"ip": j["query"], "country": j["countryCode"]}
        except Exception:
            if i == 4:
                raise
            page.wait_for_timeout(2000 * i)


def session(pw, body, work):
    s = launch({"headless": True, "record": False, **body})
    browser = pw.chromium.connect(s["wsUrl"], headers={"x-api-key": KEY})
    try:
        return {"weight": s["weight"], **work(browser.new_page())}
    finally:
        browser.close()  # ends the session and stops the clock


def scrape(page):
    exit = exit_of(page)
    page.goto("https://books.toscrape.com/", timeout=45000)
    books = page.eval_on_selector_all("article.product_pod", """els => els.map(e => ({
        title: e.querySelector('h3 a').getAttribute('title'),
        price: e.querySelector('.price_color').textContent.trim(),
    }))""")
    return {"exit": exit, "books": len(books), "first_book": books[0]}


# 1. What the free plan includes.
sub = requests.get("https://cdpfleet.com/v1/me", headers={"x-api-key": KEY}, timeout=30).json()["subscription"]
plan = {
    "plan": sub["plan_id"], "threads": sub["threads"], "minutes_per_thread_per_day": sub["daily_minutes_per_thread"],
    "max_session_minutes": sub["max_session_seconds"] // 60, "max_pages": sub["features"]["max_pages"],
    "memory_mb": sub["features"]["memory_mb"], "proxy_options": (sub.get("plan_proxy") or {}).get("options"),
}

with sync_playwright() as pw:
    # 2. No proxy in the launch: the plan's included proxy is used. Scrape a catalogue page.
    first = session(pw, {}, scrape)
    # 3. A cdpfleet-plan token picks a country and keeps the same exit across sessions.
    token = "cdpfleet-plan-country-de-session-c47" + "".join(random.choices(string.ascii_lowercase + string.digits, k=5)) + "-lifetime-10"
    a = session(pw, {"proxy": token}, lambda page: {"exit": exit_of(page)})
    b = session(pw, {"proxy": token}, lambda page: {"exit": exit_of(page)})

print(json.dumps({
    "plan": plan,
    "default_proxy": {"weight": first["weight"], "exit_country": first["exit"]["country"], "exit_ip": first["exit"]["ip"],
                      "books": first["books"], "first_book": first["first_book"]},
    "plan_token": {"token": re.sub(r"session-\w+", "session-…", token), "first_country": a["exit"]["country"],
                   "second_country": b["exit"]["country"], "same_ip": a["exit"]["ip"] == b["exit"]["ip"]},
}, indent=2, ensure_ascii=False))
