# SDK smoke test on production: launch -> connect -> page -> live link -> keep_alive reconnect -> close.
# env: CDPFLEET_API_KEY (a dedicated plan for keep_alive), PROXY_URL. Prints one JSON line.
import json
import os

from playwright.sync_api import sync_playwright

from cdpfleet import Cdpfleet

fleet = Cdpfleet()
out = {}
with sync_playwright() as p:
    s1 = fleet.launch("chromium", proxy=os.environ.get("PROXY_URL"), headless=True, live_view=True)
    b1 = s1.connect(p.chromium)
    p1 = b1.new_page()
    p1.goto("https://httpbin.org/html")
    out["title"] = p1.locator("h1").text_content()
    out["live_protocol"] = s1.live_link("view")["protocol"]
    s1.close()

    s2 = fleet.launch("chromium", proxy=os.environ.get("PROXY_URL"), headless=True, keep_alive=True, inactivity_timeout="2m")
    b2 = s2.connect(p.chromium)
    p2 = b2.new_context().new_page()
    p2.goto("https://httpbin.org/html")
    b2.close()
    b2 = s2.connect(p.chromium)
    out["keep_alive_page_kept"] = any("httpbin" in pg.url for c in b2.contexts for pg in c.pages)
    s2.close()

out["ended"] = s2.id not in json.dumps(fleet.threads())
out["usage_ok"] = isinstance(fleet.usage().get("used_thread_seconds"), (int, float))
print(json.dumps(out, separators=(",", ":")))
