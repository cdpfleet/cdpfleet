# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import struct
import time
from datetime import date, datetime, timedelta, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]  # the same proxy for the Function calls and the sessions
API = "https://cdpfleet.com"
STARTER = "https://starter.cdpfleet.com"
HEADERS = {"x-api-key": KEY}
PAGES = [
    ("example.com", "https://example.com/"),
    ("books.toscrape.com", "https://books.toscrape.com/"),
    ("wikipedia", "https://en.wikipedia.org/wiki/Web_scraping"),
]
TASKS = ["screenshot", "pdf", "markdown"]
SINCE = (datetime.now(timezone.utc) - timedelta(minutes=1)).isoformat()


def ts(s):
    return datetime.fromisoformat(s.replace("Z", "+00:00")).timestamp()


def billed(sid):
    # The record of a session or a Function call in the account's history (a call's x-call-id is its
    # session id there). Billed thread-seconds = (ended_at - started_at) × weight.
    for _ in range(40):
        r = requests.get(f"{API}/v1/me/sessions", headers=HEADERS, params={"from": SINCE, "limit": 200}, timeout=30)
        s = next((x for x in r.json().get("sessions", []) if x["session_id"] == sid), None)
        if s and s.get("ended_at"):
            secs = (ts(s["ended_at"]) - ts(s["started_at"])) * s["weight"]
            return {"engine": s["engine"], "weight": s["weight"], "thread_seconds": round(secs, 1)}
        time.sleep(2)
    return None


def describe(task, buf):
    # What a file is: PNG width × height; for a PDF, whether it has fonts (a text PDF), its page objects
    # (0 when they sit in compressed object streams) and the tallest image in it.
    if task == "screenshot":
        w, h = struct.unpack(">II", buf[16:24])
        return {"width": w, "height": h}
    if task == "pdf":
        s = buf.decode("latin1")
        heights = [int(m) for m in re.findall(r"/Height (\d+)", s)]
        return {"text_pdf": bool(re.search(r"/Font\b", s)), "pages": len(re.findall(r"/Type\s*/Page(?![s\w])", s)),
                "image_height": max(heights) if heights else 0}
    return {}


def via_function(task, url):
    # (a) The Function: one POST, the file in the response. One retry if the call fails.
    body = {"url": url, "proxy": PROXY}
    if task == "screenshot":
        body["full_page"] = True
    if task == "pdf":
        body["format"] = "A4"
    t = time.time()
    calls = []
    for _ in range(2):
        res = requests.post(f"{API}/v1/functions/{task}", headers=HEADERS, json=body, timeout=40)
        if res.headers.get("x-call-id"):
            calls.append(res.headers["x-call-id"])
        if res.status_code == 200:
            break
    ms = int((time.time() - t) * 1000)
    buf = res.content
    records = [billed(c) for c in calls]
    out = {
        "status": res.status_code, "attempts": len(calls), "ms": ms, "bytes": len(buf),
        "content_type": res.headers.get("content-type"),
        "partial": res.headers.get("x-cdpfleet-partial"),
        "engine": records[-1]["engine"] if records and records[-1] else None,
        "weight": records[-1]["weight"] if records and records[-1] else None,
        "thread_seconds": round(sum(r["thread_seconds"] for r in records), 1) if all(records) else None,
    }
    if res.status_code == 200:
        out.update(describe(task, buf))
        if task == "markdown":
            md = buf.decode("utf-8")
            out["first_line"] = md.split("\n")[0][:80]
            out["has_source_line"] = "\nSource: " in md
    return out


def launch(pw):
    # (b) Your own session: launch over HTTP, connect with the key header.
    res = requests.post(f"{STARTER}/chromium/session", headers=HEADERS, json={"proxy": PROXY, "headless": True}, timeout=90)
    if res.status_code != 200:
        raise RuntimeError(f"launch {res.status_code} {res.text}")
    s = res.json()
    return s["sessionId"], pw.chromium.connect(s["wsUrl"], headers=HEADERS)


def open_page(browser, url):
    page = browser.new_page(viewport={"width": 1280, "height": 720})
    try:
        page.goto(url, timeout=45000)
    except Exception:  # a proxy drops the odd connection: one retry
        page.goto(url, timeout=45000)
    return page


def capture(page, task):
    # Markdown has no Playwright call: you'd add a converter (markdownify, html2text, …). Measured here:
    # the HTML you would convert and the page's innerText.
    if task == "screenshot":
        buf = page.screenshot(full_page=True)
        return {"bytes": len(buf), **describe(task, buf)}
    if task == "pdf":
        buf = page.pdf(format="A4")
        return {"bytes": len(buf), **describe(task, buf)}
    html = len(page.content().encode("utf-8"))
    text = len(page.locator("body").inner_text().encode("utf-8"))
    return {"bytes": html, "html_bytes": html, "text_bytes": text}


def via_session(pw, task, url):
    t = time.time()
    sid, browser = launch(pw)
    launch_ms = int((time.time() - t) * 1000)
    try:
        out = capture(open_page(browser, url), task)
    finally:
        browser.close()  # closing the connection ends the session
    ms = int((time.time() - t) * 1000)
    rec = billed(sid) or {}
    return {"ms": ms, "launch_ms": launch_ms, "engine": rec.get("engine"), "weight": rec.get("weight"),
            "thread_seconds": rec.get("thread_seconds"), **out}


with sync_playwright() as pw:
    runs = []
    for name, url in PAGES:
        for task in TASKS:
            runs.append({"page": name, "task": task, "function": via_function(task, url), "session": via_session(pw, task, url)})

    # (c) One session for all nine captures: launch once, three pages, three outputs each.
    t = time.time()
    sid, browser = launch(pw)
    captures = 0
    try:
        for _, url in PAGES:
            page = open_page(browser, url)
            for task in TASKS:
                capture(page, task)
                captures += 1
            page.close()
    finally:
        browser.close()
    one_ms = int((time.time() - t) * 1000)
    one = billed(sid) or {}

print(json.dumps({
    "language": "python",
    "proxy": "the same PROXY_URL for every Function call and every session",
    "runs": runs,
    "one_session": {"captures": captures, "ms": one_ms, "thread_seconds": one.get("thread_seconds"), "weight": one.get("weight")},
    "measured_at": date.today().isoformat(),
}, indent=2))
