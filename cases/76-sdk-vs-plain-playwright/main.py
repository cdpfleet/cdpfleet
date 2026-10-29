# pip install playwright==1.60.0 requests cdpfleet==0.1.1
# env: CDPFLEET_API_KEY, PROXY_URL, COMMUNITY_API_KEY (a Community account: 2 threads, to reach the cap)
import json
import os
import threading
import time
from datetime import date

import requests
from cdpfleet import Cdpfleet, CdpfleetError
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
FREE_KEY = os.environ["COMMUNITY_API_KEY"]
PROXY = os.environ["PROXY_URL"]
STARTER = "https://starter.cdpfleet.com"
API = "https://cdpfleet.com"
URLS = [f"https://books.toscrape.com/catalogue/page-{n}.html" for n in (1, 2, 3)]


# The scrape every variant runs: three catalogue pages, every book title.
def scrape(browser):
    page = browser.new_page()
    titles = []
    for url in URLS:
        try:
            page.goto(url, timeout=45000)
        except Exception:  # a shared proxy drops the odd connection: one retry
            page.goto(url, timeout=45000)
        titles += page.eval_on_selector_all("article.product_pod h3 a", "as => as.map(a => a.getAttribute('title'))")
    return {"books": len(titles), "first_title": titles[0]}


# ---- 1a. Plain Playwright: launch over HTTP yourself, then connect with the key header.
def plain_launch(key, body):
    res = requests.post(f"{STARTER}/chromium/session", headers={"x-api-key": key}, json=body, timeout=60)
    try:
        data = res.json()
    except ValueError:
        data = None
    return res.status_code, res.headers.get("retry-after"), data


def plain(p):
    t = time.time()
    status, _, body = plain_launch(KEY, {"proxy": PROXY, "headless": True, "record": False})
    if status != 200:
        raise RuntimeError(f"launch {status} {body}")
    browser = p.chromium.connect(body["wsUrl"], headers={"x-api-key": KEY})
    connected_ms = round((time.time() - t) * 1000)
    try:
        return {**scrape(browser), "connected_ms": connected_ms, "seconds": round(time.time() - t, 3)}
    finally:
        browser.close()  # closing the connection ends the session


# ---- 1b. The SDK: launch() (retries 429/503 as the API asks), connect(), close() via `with`.
fleet = Cdpfleet()  # reads CDPFLEET_API_KEY


def with_sdk(p):
    t = time.time()
    with fleet.launch("chromium", proxy=PROXY, headless=True, record=False) as session:
        browser = session.connect(p.chromium)
        connected_ms = round((time.time() - t) * 1000)
        return {**scrape(browser), "connected_ms": connected_ms, "seconds": round(time.time() - t, 3)}


# ---- 1c. The SDK's connect URL: one connect() launches the browser (no HTTP call of your own).
def with_connect_url(p):
    t = time.time()
    url = fleet.connect_url("chromium", proxy=PROXY, headless=True, record=False)
    browser = p.chromium.connect(url, headers={"x-api-key": fleet.api_key})
    connected_ms = round((time.time() - t) * 1000)
    try:
        return {**scrape(browser), "connected_ms": connected_ms, "seconds": round(time.time() - t, 3)}
    finally:
        browser.close()


# ---- 2. At the thread cap (Community: 2 threads). Hold both, then ask for a third.
def end_session(key, sid):
    requests.delete(f"{API}/v1/me/threads/{sid}", headers={"x-api-key": key}, timeout=30)


def live_threads(key):
    return len(requests.get(f"{API}/v1/me/threads", headers={"x-api-key": key}, timeout=30).json()["threads"])


def at_the_cap():
    held = []
    for _ in range(2):
        status, _, body = plain_launch(FREE_KEY, {"headless": True, "record": False})
        if status != 200:
            raise RuntimeError(f"hold {status} {body}")
        held.append(body["sessionId"])
        time.sleep(1.5)  # Community launches: at most 2 a second, 12 a minute
    # Plain: the answer comes back at once and the retry is yours to write.
    status, retry_after, body = plain_launch(FREE_KEY, {"headless": True, "record": False})
    time.sleep(1.5)
    # SDK: the same launch waits; free a thread 5 s later and see when it returns.
    free_fleet = Cdpfleet(api_key=FREE_KEY)
    threading.Timer(5, end_session, (FREE_KEY, held.pop(0))).start()
    t = time.time()
    try:
        s = free_fleet.launch("chromium", headless=True, record=False)
        retry = {"by": "sdk", "got_session": True, "waited_s": round(time.time() - t, 3)}
        s.end()  # never connected: end it explicitly
    except CdpfleetError as err:
        retry = {"by": "sdk", "got_session": False, "waited_s": round(time.time() - t, 3), "error": err.code}
    for sid in held:
        end_session(FREE_KEY, sid)
    time.sleep(3)
    return {
        "threads": 2,
        "plain": {"status": status, "error": (body or {}).get("error"), "retry_after": retry_after,
                  "in_use": (body or {}).get("in_use"), "upgrade_url": ((body or {}).get("upgrade") or {}).get("url")},
        "retry": retry,
        "threads_left": live_threads(FREE_KEY),
    }


with sync_playwright() as p:
    out = {"language": "python", "sdk": "cdpfleet 0.1.1 (PyPI)", "scrape": {}}
    out["scrape"]["plain"] = plain(p)
    out["scrape"]["sdk"] = with_sdk(p)
    out["scrape"]["connect_url"] = with_connect_url(p)
    out["cap"] = at_the_cap()
    out["measured_at"] = date.today().isoformat()
    print(json.dumps(out, indent=2))
