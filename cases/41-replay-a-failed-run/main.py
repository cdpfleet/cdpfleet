# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from urllib.parse import urljoin

import requests
from playwright.sync_api import Error as PlaywrightError, TimeoutError as PlaywrightTimeout, sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITE = "https://quotes.toscrape.com"
NEXT = "li.next a"  # the scraper assumes every listing page has a "Next" link


def api(method, path):
    res = requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=30)
    try:
        body = res.json()
    except ValueError:
        body = {}
    return res.status_code, body


# Headful (the default) sessions are recorded from the browser's display.
def launch(p, **extra):
    res = requests.post(
        "https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": False, **extra},
        timeout=60,
    )
    if not res.ok:
        raise RuntimeError(f"launch: {res.status_code} {res.text}")
    body = res.json()
    started = time.time()
    browser = p.chromium.connect(body["wsUrl"], headers={"x-api-key": KEY})
    # no_viewport: the page fills the whole display, so it fills the recording too.
    page = browser.new_context(no_viewport=True).new_page()
    return body["sessionId"], started, browser, page


# Proxies drop a tunnel now and then: up to two retries.
def go(page, url):
    for attempt in range(1, 4):
        try:
            return page.goto(url, timeout=60000)
        except PlaywrightError:
            if attempt == 3:
                raise
            time.sleep(2)


# The recording appears once the node has uploaded it, after the session ended.
def wait_for_recording(session_id, max_seconds):
    t = time.time()
    while time.time() - t < max_seconds:
        recs = api("GET", "/v1/me/recordings")[1].get("recordings") or []
        rec = next((r for r in recs if r["session_id"] == session_id), None)
        if rec:
            return rec, round(time.time() - t)
        time.sleep(5)
    return None, round(time.time() - t)


out = {"site": SITE, "scrape": {}, "recording": {}, "unrecorded": {}}
with sync_playwright() as p:
    # A session launched with record: false, for comparison: it must never get a recording.
    quiet_id, _, quiet_browser, quiet_page = launch(p, record=False)
    try:
        go(quiet_page, SITE)
    finally:
        quiet_browser.close()
    out["unrecorded"]["session_id"] = quiet_id

    # The scrape: pages 8, 9, 10 — follow "Next" on each. Page 10 is the last one and has none.
    session_id, started, browser, page = launch(p)
    out["session_id"] = session_id
    quotes = []
    try:
        go(page, f"{SITE}/page/8/")
        page_no = 8
        while True:
            quotes += page.locator(".quote .text").all_text_contents()
            out["scrape"]["last_page"] = page_no
            try:
                href = page.locator(NEXT).get_attribute("href", timeout=5000)  # 5 s to find the link
            except PlaywrightError as err:
                out["scrape"]["failed_at_s"] = round(time.time() - started, 1)
                kind = "timeout" if isinstance(err, PlaywrightTimeout) else type(err).__name__
                out["scrape"]["error"] = f"{kind} waiting for {NEXT}"
                break
            go(page, urljoin(SITE, href))  # page loads keep their own timeout and retries
            page_no += 1
        out["scrape"]["quotes"] = len(quotes)
        # What the video shows at that moment: the last page — a "Previous" link and no "Next".
        out["scrape"]["url_at_failure"] = page.url
        out["scrape"]["next_links"] = page.locator("li.next").count()
        out["scrape"]["previous_links"] = page.locator("li.previous").count()
    finally:
        browser.close()  # ends the session; the node uploads the video afterwards
    out["session_seconds"] = round(time.time() - started, 1)

# Find the recording, check it covers the failure, download it, delete it.
rec, waited = wait_for_recording(session_id, 600)
if rec:
    link = api("GET", f"/v1/me/recordings/{rec['id']}/url")[1]
    video = requests.get(link["url"], timeout=60)
    out["recording"] = {
        "appeared_after_s": waited,
        "duration_seconds": rec["duration_seconds"],
        "width": rec["width"],
        "height": rec["height"],
        "bytes": rec["bytes"],
        "downloaded_bytes": len(video.content),
        "content_type": video.headers.get("content-type"),
        "mp4": video.content[4:8] == b"ftyp",
        "link_expires_in_s": link["expires_in_s"],
    }
    out["failure_within_recording"] = out["scrape"]["failed_at_s"] <= rec["duration_seconds"] + 1
    out["recording"]["deleted"] = api("DELETE", f"/v1/me/recordings/{rec['id']}")[0] == 200

# By now the scrape's video has been uploaded; give the unrecorded session another minute.
time.sleep(60)
recs = api("GET", "/v1/me/recordings")[1].get("recordings") or []
out["unrecorded"]["recording_found"] = any(r["session_id"] == quiet_id for r in recs)
print(json.dumps(out, indent=2))
