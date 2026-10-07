# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import tempfile
import threading
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
DIR = tempfile.mkdtemp(prefix="cdpfleet-live-")

TARGETS = [
    {"browser": "chromium", "body": {"headless": "new"}},
    {"browser": "firefox", "body": {"headless": True}},
]


def watch(p, target):
    engine = target["browser"]
    for attempt in range(1, 6):  # 503 = momentarily no capacity for this browser
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, timeout=60,
                            json={"proxy": os.environ["PROXY_URL"], **target["body"]})
        if res.status_code != 503:
            break
        time.sleep(2 * attempt)
    if not res.ok:
        return {"browser": engine, "error": f"launch {res.status_code} {res.text}"}
    browser_type = p.chromium if engine == "chromium" else p.firefox
    browser = browser_type.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        frames = []  # every frame, tagged with the phase it arrived in
        lock = threading.Lock()
        state = {"phase": "load"}

        # Every frame arrives here as JPEG bytes — this is where a viewer would get it.
        def on_frame(frame):
            data = frame["data"]
            with lock:
                frames.append({"phase": state["phase"], "bytes": len(data), "jpeg": data[:2] == b"\xff\xd8",
                               "w": frame["viewportWidth"], "h": frame["viewportHeight"], "data": data})

        page.screencast.start(quality=60, on_frame=on_frame)
        for attempt in range(1, 4):  # the proxy can drop a tunnel; retry
            try:
                page.goto("https://en.wikipedia.org/wiki/Web_browser", timeout=60000)
                break
            except Exception:
                if attempt == 3:
                    raise
        state["phase"] = "scroll"
        t = time.time()
        for _ in range(10):
            page.mouse.wheel(0, 500)
            page.wait_for_timeout(400)
        scrolling = time.time() - t
        state["phase"] = "idle"  # nothing changes on the page now
        page.wait_for_timeout(3000)
        page.screencast.stop()

        with lock:
            got = list(frames)
        count = lambda phase: sum(1 for f in got if f["phase"] == phase)
        last = got[-1] if got else None
        if last:
            with open(os.path.join(DIR, f"{engine}.jpg"), "wb") as fh:
                fh.write(last["data"])
        return {
            "browser": engine,
            "frames_while_loading": count("load"),
            "frames_while_scrolling": count("scroll"),
            "fps_while_scrolling": round(count("scroll") / scrolling, 1),
            "frames_in_3s_idle": count("idle"),
            "avg_kb": round(sum(f["bytes"] for f in got) / max(len(got), 1) / 1024),
            "all_jpeg": all(f["jpeg"] for f in got),
            "viewport": f"{last['w']}x{last['h']}" if last else None,
        }
    finally:
        browser.close()


with sync_playwright() as p:
    out = [watch(p, t) for t in TARGETS]
print(json.dumps(out, indent=2))
