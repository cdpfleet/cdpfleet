# pip install playwright==1.60.0 requests
# env: CDPFLEET_DEDICATED_API_KEY (keep_alive needs dedicated threads), PROXY_URL
import json
import os
import time
from datetime import date

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_DEDICATED_API_KEY"]
PROXY = os.environ["PROXY_URL"]
STARTER = "https://starter.cdpfleet.com"
API = "https://cdpfleet.com"
IDLE_S = 30  # inactivity_timeout: how long the session waits for a client
SITE = "https://books.toscrape.com"
HEADERS = {"x-api-key": KEY}


def api(path):
    return requests.get(f"{API}{path}", headers=HEADERS, timeout=30).json()


def listed(sid):
    # The session as the account API sees it while it runs: listed in /v1/me/threads or not.
    return next((t for t in api("/v1/me/threads")["threads"] if t["session_id"] == sid), None)


def usage():
    # Today's thread-seconds on the account, closed and still running.
    u = api("/v1/me/usage")
    return {"closed": u["closed_thread_seconds"], "live": u["live_thread_seconds"]}


def goto(page, url):
    try:
        page.goto(url, timeout=45000)
    except Exception:  # a shared proxy drops the odd connection: one retry
        page.goto(url, timeout=45000)


def secs(a, b):
    return round(b - a, 1)


usage_before = usage()

# 1. Launch with keep_alive: a disconnect no longer ends the session.
t0 = time.time()
res = requests.post(f"{STARTER}/chromium/session", headers=HEADERS, timeout=90,
                    json={"proxy": PROXY, "headless": "new", "keep_alive": True, "inactivity_timeout": f"{IDLE_S}s"})
if not res.ok:
    raise SystemExit(f"launch failed: {res.status_code} {res.text}")
session = res.json()
sid, ws_url = session["sessionId"], session["wsUrl"]

with sync_playwright() as pw:
    # 2. First client: an agent works through the catalogue in one context, a second context on the side.
    browser = pw.chromium.connect(ws_url, headers=HEADERS)
    work = browser.new_context()
    page = work.new_page()
    goto(page, f"{SITE}/catalogue/page-1.html")
    page.click("li.next a")  # step 2 of the run: page 2
    work.add_cookies([{"name": "agent_step", "value": "2", "domain": "books.toscrape.com", "path": "/"}])
    page.evaluate("() => localStorage.setItem('agent_state', JSON.stringify({ seen: 40, next: 'page-3' }))")
    detail = work.new_page()
    goto(detail, f"{SITE}/catalogue/a-light-in-the-attic_1000/index.html")
    side = browser.new_context()
    goto(side.new_page(), f"{SITE}/catalogue/category/books/poetry_23/index.html")
    before = {"contexts": len(browser.contexts), "pages": sum(len(c.pages) for c in browser.contexts), "url": page.url}

    # 3. The client goes away. With keep_alive, close() only disconnects.
    browser.close()
    t_detach = time.time()
    time.sleep(3)
    while_detached = listed(sid)
    time.sleep(12)

    # 4. A new client reconnects to the same wsUrl and carries on where the first one stopped.
    t_reconnect = time.time()
    browser = pw.chromium.connect(ws_url, headers=HEADERS)
    reconnect_ms = round((time.time() - t_reconnect) * 1000)
    detached_s = secs(t_detach, t_reconnect)
    contexts = browser.contexts
    pages = [(c, p) for c in contexts for p in c.pages]
    back = next(((c, p) for c, p in pages if "page-2.html" in p.url), None)
    after = {
        "contexts": len(contexts),
        "pages": len(pages),
        "urls": sorted(p.url.replace(SITE, "") for _, p in pages),
        "cookie": next((c["value"] for c in back[0].cookies(SITE) if c["name"] == "agent_step"), None) if back else None,
        "local_storage": back[1].evaluate("() => localStorage.getItem('agent_state')") if back else None,
    }
    continued = None
    if back:
        back[1].click("li.next a")
        back[1].wait_for_url("**/page-3.html")
        continued = back[1].url.replace(SITE, "")

    # 5. The client leaves for good: the session waits inactivity_timeout, then ends.
    browser.close()
    t_leave = time.time()
    gone_after_s = None
    usage_waiting = None
    while time.time() - t_leave < IDLE_S + 90:
        time.sleep(2)
        if usage_waiting is None and time.time() - t_leave >= 20:
            usage_waiting = usage()
        if not listed(sid):
            gone_after_s = secs(t_leave, time.time())
            break
    try:
        b = pw.chromium.connect(ws_url, headers=HEADERS, timeout=20000)
        late_reconnect = {"connected": True, "contexts": len(b.contexts)}
        b.close()
    except Exception as err:
        late_reconnect = {"connected": False, "error": str(err).split("\n")[0][:160]}

# 6. What was billed: the session's record in the account history.
time.sleep(5)
s = next((x for x in api("/v1/me/sessions?limit=20")["sessions"] if x["session_id"] == sid), {})
usage_after = usage()

print(json.dumps({
    "language": "python",
    "inactivity_timeout_s": IDLE_S,
    "before": before,
    "detached": {"listed": bool(while_detached), "running_seconds": (while_detached or {}).get("running_seconds"), "seconds": detached_s},
    "reconnect": {"ms": reconnect_ms, **after, "continued": continued},
    "end": {
        "gone_after_s": gone_after_s,
        "late_reconnect": late_reconnect,
        "end_reason": s.get("end_reason"),
        "duration_seconds": s.get("duration_seconds"),
        "weight": s.get("weight"),
        "client_seconds": secs(t0, t_leave),
    },
    "usage": {"live_while_waiting": usage_waiting["live"] if usage_waiting else None, "billed": usage_after["closed"] - usage_before["closed"]},
    "measured_at": date.today().isoformat(),
}, indent=2))
