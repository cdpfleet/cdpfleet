# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (your plan), PROXY_URL; optional COMMUNITY_API_KEY, CDPFLEET_DEDICATED_API_KEY to compare plans
import json
import os
import re
import time
from datetime import date

import requests
from playwright.sync_api import sync_playwright

STARTER = "https://starter.cdpfleet.com"
API = "https://cdpfleet.com"
PAGE = "https://en.wikipedia.org/wiki/World_War_II"  # a long article: a mid-weight page
MAX_TABS = 50  # the per-session tab cap on every plan
PROXY = os.environ.get("PROXY_URL")

# Each run: an account, an engine and a memory multiplier. Runs whose key isn't set are skipped.
RUNS = [r for r in [
    ("COMMUNITY_API_KEY", "chromium", 1),
    ("CDPFLEET_API_KEY", "chromium", 1),
    ("CDPFLEET_API_KEY", "firefox", 1),
    ("CDPFLEET_API_KEY", "webkit", 1),
    ("CDPFLEET_API_KEY", "chromium", 2),
    ("CDPFLEET_DEDICATED_API_KEY", "chromium", 1),
] if os.environ.get(r[0])]


def api(key, path):
    return requests.get(f"{API}{path}", headers={"x-api-key": key}, timeout=30).json()


def first_line(err):
    return str(err).split("\n")[0]


def run(p, env, engine, memory):
    key = os.environ[env]
    sub = api(key, "/v1/me")["subscription"]
    plan = "Community" if (sub.get("features") or {}).get("community") else "Dedicated" if sub.get("kind") == "dedicated" else "Shared"
    t0 = time.time()
    # Community launches use the plan's included proxy; paid plans bring their own.
    body = {"headless": True}
    if memory > 1:
        body["memory"] = memory
    if plan != "Community":
        body["proxy"] = PROXY
    res = requests.post(f"{STARTER}/{engine}/session", headers={"x-api-key": key}, json=body, timeout=120)
    if not res.ok:
        return {"plan": plan, "engine": engine, "memory": memory, "launch_error": f"{res.status_code} {res.text}"}
    launch = res.json()
    browser = getattr(p, engine).connect(launch["wsUrl"], headers={"x-api-key": key})
    context = browser.new_context()
    # Open the article in one new tab after another until the session ends or the tab cap is reached.
    tabs = 0
    client_error = None
    while tabs < MAX_TABS:
        try:
            page = context.new_page()
            try:
                page.goto(PAGE, timeout=60000)
            except Exception as err:  # a slow load through a shared proxy gets one more try; a closed session doesn't
                if "Timeout" not in str(err):
                    raise
                page.goto(PAGE, timeout=60000)
            tabs += 1
        except Exception as err:
            client_error = first_line(err)
            break
    # At the cap with memory to spare: what does a 51st tab get?
    tab51 = None
    if tabs == MAX_TABS:
        try:
            context.new_page()
            tab51 = "opened"
        except Exception as err:
            tab51 = first_line(err)
    try:
        browser.close()
    except Exception:
        pass
    # The session's record: why it ended, its weight, peak memory and time.
    record = None
    for _ in range(20):
        if record and record.get("ended_at"):
            break
        time.sleep(1.5)
        record = next((s for s in api(key, "/v1/me/sessions?limit=20")["sessions"] if s["session_id"] == launch["sessionId"]), None)
    if client_error:
        m = re.search(r"(Session closed: .*)$", client_error)
        client_error = m.group(1) if m else client_error
    return {
        "plan": plan, "engine": engine, "memory": memory, "weight": launch["weight"], "memory_mb": launch["memory_mb"], "tabs": tabs,
        "client_error": client_error, "tab_51": tab51,
        "end_reason": record.get("end_reason") if record else None,
        "memory_peak_mb": record.get("memory_peak_mb") if record else None,
        "seconds": record["duration_seconds"] if record else round(time.time() - t0),
        "thread_seconds": record["duration_seconds"] * record["weight"] if record else None,
    }


with sync_playwright() as p:
    runs = [run(p, *r) for r in RUNS]  # one at a time: one site, one session at a time
print(json.dumps({"language": "python", "page": PAGE, "max_tabs": MAX_TABS, "measured_at": date.today().isoformat(), "runs": runs}, indent=2))
