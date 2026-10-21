# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]

# Six ways to drive a remote browser. Chromium (Playwright's build) and Patchright share a
# Chromium base, so they're the like-for-like pair; Chrome is also driven over CDP, with and
# without a console listener; Camoufox is Firefox, driven over Playwright's Juggler, not CDP.
SETUPS = [
    {"label": "Chromium, Playwright connect", "engine": "chromium", "family": "chromium", "cdp": False, "listen": True},
    {"label": "Patchright, Playwright connect", "engine": "patchright", "family": "chromium", "cdp": False, "listen": True},
    {"label": "Chrome, Playwright connect", "engine": "chrome", "family": "chromium", "cdp": False, "listen": True},
    {"label": "Chrome, connectOverCDP", "engine": "chrome", "family": "chromium", "cdp": True, "listen": True},
    {"label": "Chrome, connectOverCDP, no console listener", "engine": "chrome", "family": "chromium", "cdp": True, "listen": False},
    {"label": "Camoufox, Playwright connect", "engine": "camoufox", "family": "firefox", "cdp": False, "listen": True},
]

# What a detection script can do from inside the page, run as the page's own script:
#  - the classic tell: log an Error whose `stack` is a getter; a debugger client that previews
#    logged objects used to read it;
#  - an Error whose `message` is a getter, the same idea;
#  - the cost of console.log: with a client listening to the console, every logged object is
#    serialized for it. Logging a big object 40 times is timed against JSON.stringify of the
#    same object (so the machine's speed cancels out), five times; the median ratio is kept.
# The verdict is left in an attribute that every client can read.
PAGE = """<html><body><p>console probe</p><script>
  const out = { stack_getter_read: false, message_getter_read: false };
  const e1 = new Error('probe');
  Object.defineProperty(e1, 'stack', { get() { out.stack_getter_read = true; return ''; } });
  console.debug(e1);
  const e2 = new Error('probe');
  Object.defineProperty(e2, 'message', { get() { out.message_getter_read = true; return ''; } });
  console.log(e2);
  console.log('hello-from-page');
  const big = Array.from({ length: 1000 }, (_, i) => ({ i, s: 'x'.repeat(40), a: [1, 2, 3, { d: i }] }));
  const ratios = [];
  for (let k = 0; k < 5; k++) {
    let t = performance.now();
    for (let i = 0; i < 40; i++) console.log(big);
    const logMs = performance.now() - t;
    t = performance.now();
    for (let i = 0; i < 40; i++) JSON.stringify(big);
    ratios.push(logMs / (performance.now() - t));
  }
  ratios.sort((a, b) => a - b);
  out.log_cost_ratio = Math.round(ratios[2] * 100) / 100;
  setTimeout(() => document.documentElement.setAttribute('data-out', JSON.stringify(out)), 1000);
</script></body></html>"""


def run(p, s):
    body = {"proxy": PROXY, "headless": True, "record": False, **({"cdp": True} if s["cdp"] else {})}
    res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=60)
    if not res.ok:
        return {"label": s["label"], "error": f"launch {res.status_code} {res.text}"}
    session = res.json()
    browser_type = getattr(p, s["family"])
    browser = (browser_type.connect_over_cdp(session["cdpUrl"], headers={"x-api-key": KEY}) if s["cdp"]
               else browser_type.connect(session["wsUrl"], headers={"x-api-key": KEY}))
    try:
        context = browser.contexts[0] if s["cdp"] else browser.new_context()
        page = context.new_page()
        events = [0]
        if s["listen"]:
            page.on("console", lambda m: events.__setitem__(0, events[0] + ("hello-from-page" in m.text)))
        page.set_content(PAGE)
        page.wait_for_selector("html[data-out]", timeout=30000)
        probe = json.loads(page.evaluate("() => document.documentElement.getAttribute('data-out')"))
        return {
            "label": s["label"], "engine": s["engine"], "connect": "connectOverCDP" if s["cdp"] else "connect",
            "console_listener": s["listen"], "console_events": events[0] if s["listen"] else None, **probe,
            "evaluate_works": page.evaluate("() => 6 * 7") == 42,
        }
    finally:
        browser.close()


with sync_playwright() as p:
    setups = [run(p, s) for s in SETUPS]  # one at a time: an idle session ends after 2 minutes by default
print(json.dumps({"setups": setups}, indent=2))
