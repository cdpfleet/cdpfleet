# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import tempfile
import time
import zipfile

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
DIR = tempfile.mkdtemp(prefix="cdpfleet-replay-")

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, timeout=60,
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"})
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")
ws_url = res.json()["wsUrl"]

t = time.time()
with sync_playwright() as p:
    browser = p.chromium.connect(ws_url, headers={"x-api-key": KEY})
    try:
        # Video is recorded on the remote browser and fetched when the page closes; the trace is
        # assembled by the client from events and screenshots streamed over the same connection.
        context = browser.new_context(record_video_dir=DIR, record_video_size={"width": 1280, "height": 720},
                                      viewport={"width": 1280, "height": 720})
        context.tracing.start(screenshots=True, snapshots=True)
        page = context.new_page()
        video = page.video  # take the handle while the page is open
        page.goto("https://example.com/", timeout=60000)
        page.goto("https://httpbin.org/forms/post", timeout=60000)
        page.get_by_label("Customer name").fill("Ada Lovelace")
        page.get_by_label("Large").check()
        page.get_by_role("button", name="Submit order").click()
        page.wait_for_url("**/post", timeout=60000)  # httpbin echoes the form as JSON
        page.screenshot(path=os.path.join(DIR, "final.png"))
        context.tracing.stop(path=os.path.join(DIR, "trace.zip"))
        context.close()  # finishes the video
        video.save_as(os.path.join(DIR, "session.webm"))
    finally:
        browser.close()
seconds = time.time() - t

trace_path = os.path.join(DIR, "trace.zip")
with zipfile.ZipFile(trace_path) as z:
    names = z.namelist()
    events = [json.loads(line) for n in names if n.endswith(".trace")
              for line in z.read(n).decode().split("\n") if line]
    network = sum(1 for n in names if n.endswith(".network") for line in z.read(n).decode().split("\n") if line)
actions = [e["method"] for e in events if e.get("type") == "before" and e.get("method")]
webm = open(os.path.join(DIR, "session.webm"), "rb").read()
empty = {"screenshots": None, "snapshots": None, "network_entries": None}

print(json.dumps([
    {"artifact": "trace.zip", "bytes": os.path.getsize(trace_path), "detail": f"{len(actions)} actions: {', '.join(actions)}",
     "screenshots": sum(1 for n in names if n.startswith("resources/") and n.endswith(".jpeg")),
     "snapshots": sum(1 for e in events if e.get("type") == "frame-snapshot"), "network_entries": network,
     "open_with": "npx playwright show-trace trace.zip"},
    {"artifact": "session.webm", "bytes": len(webm),
     "detail": "valid WebM (EBML header)" if webm[:4] == b"\x1a\x45\xdf\xa3" else "not a WebM file", **empty, "open_with": "any video player"},
    {"artifact": "final.png", "bytes": os.path.getsize(os.path.join(DIR, "final.png")), "detail": "screenshot after the last step", **empty, "open_with": "image viewer"},
    {"artifact": "whole run", "bytes": None, "detail": f"{seconds:.1f} s including recording and downloads", **empty, "open_with": None},
], indent=2))
