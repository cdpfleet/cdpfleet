# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, SOCKS_PROXIES (comma-separated sticky socks5:// URLs: one exit IP each)
import json
import os
import re
import time
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXIES = os.environ["SOCKS_PROXIES"].split(",")
PAGES = [f"https://httpbin.org/anything/catalogue/page-{i + 1}" for i in range(8)]
CHALLENGE = "<title>Just a moment...</title><p>Checking your browser before accessing the site.</p>"


# A stand-in for a rate-limited site, in front of real httpbin requests. Each request really goes
# out through the session's proxy; the limiter decides by the IP httpbin saw (its "origin"):
# 3 pages in a burst, then one page per 5 s (429 + Retry-After: 5), 6 pages per IP in total. An IP
# that comes back before its Retry-After, or is over its quota, gets a challenge page from then on.
def limiter():
    ips = {}

    def handle(route):
        now = time.time() * 1000  # when the request arrived
        try:
            real = route.fetch(timeout=30000)
        except Exception:
            return route.fulfill(status=502, content_type="text/plain", body="Bad Gateway")  # the proxy hiccuped
        if real.status != 200:
            return route.fulfill(response=real)  # httpbin itself failed
        s = ips.setdefault(real.json()["origin"], {"served": 0, "last": 0, "retry_at": 0, "challenged": False})
        if now < s["retry_at"] or s["served"] >= 6:
            s["challenged"] = True
        if s["challenged"]:
            return route.fulfill(status=403, content_type="text/html", body=CHALLENGE)
        if s["served"] >= 3 and now - s["last"] < 5000:
            s["retry_at"] = now + 5000
            return route.fulfill(status=429, headers={"retry-after": "5"}, content_type="text/plain", body="Too Many Requests")
        s["served"] += 1
        s["last"] = now
        return route.fulfill(response=real)

    return handle


# What the response means: the data, a rate limit (wait), or a block (this IP is done).
def classify(status, text):
    if status == 429:
        return "rate_limited"
    if status in (403, 503) or re.search(r"just a moment|captcha|access denied", text, re.I):
        return "blocked"
    return "ok" if status == 200 else "error"


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(body):
    for attempt in range(1, 6):
        r = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


def crawl(p, polite):
    session = launch({"proxy": PROXIES[1 if polite else 0], "headless": "new", "record": False})
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    site = limiter()

    # Live proxy swap on the session's router: new connections use the new proxy.
    def swap_proxy(proxy):
        r = requests.post(f"https://{urlparse(session['wsUrl']).netloc}/admin/session/proxy", headers={"x-api-key": KEY},
                          json={"session_id": session["sessionId"], "proxy": proxy}, timeout=30)
        if not r.ok:
            raise RuntimeError(f"swap {r.status_code} {r.text}")

    def open_context(storage_state=None):
        context = browser.new_context(storage_state=storage_state)
        context.route("https://httpbin.org/anything/**", site)
        return context, context.new_page()

    rows, waited, swaps, nxt, cookie_kept = [], 0, 0, 2, False
    try:
        context, page = open_context()
        # A logged-in state that must survive a proxy swap.
        page.goto("https://httpbin.org/cookies/set?session=alice-42", timeout=60000)
        for i, url in enumerate(PAGES):
            trail = []
            row = {"page": i + 1, "outcome": "gave up", "exit_ip": None}
            for attempt in range(1, 7):
                res = page.goto(url, timeout=60000)
                text = page.text_content("body")
                kind = classify(res.status, text)
                trail.append(str(res.status))
                if kind == "ok":
                    row = {"page": i + 1, "outcome": "ok", "exit_ip": json.loads(text)["origin"]}
                    break
                if kind == "rate_limited":
                    if not polite and attempt == 3:
                        break
                    # Honour Retry-After; without one, back off exponentially (1, 2, 4… s, capped).
                    seconds = int(res.headers.get("retry-after") or min(2 ** (attempt - 1), 30)) if polite else 0
                    if seconds:
                        trail.append(f"wait {seconds}s")
                    waited += seconds
                    time.sleep(seconds)
                    continue
                if kind == "blocked" and polite and nxt < len(PROXIES):
                    # This exit is done: move the session to another proxy and continue in a new context
                    # (its own connections) that keeps the cookies and storage.
                    swap_proxy(PROXIES[nxt])
                    nxt += 1
                    state = context.storage_state()
                    context.close()
                    context, page = open_context(state)
                    swaps += 1
                    trail.append("swap proxy")
                    continue
                if kind == "error":
                    time.sleep(1)  # a transient failure: try again
                    continue
                break
            rows.append({**row, "trail": " → ".join(trail)})
        page.goto("https://httpbin.org/cookies", timeout=60000)
        cookie_kept = json.loads(page.text_content("body"))["cookies"].get("session") == "alice-42"
    finally:
        browser.close()
    return {
        "client": "polite" if polite else "naive",
        "pages_fetched": sum(1 for r in rows if r["outcome"] == "ok"),
        "exits": list(dict.fromkeys(r["exit_ip"] for r in rows if r["exit_ip"])),
        "waited_seconds": waited,
        "proxy_swaps": swaps,
        "session_cookie_kept": cookie_kept,
        "pages": rows,
    }


# Naive: retries a 429 straight away and has no plan for a block. Polite: waits as told and moves
# to another exit when one is blocked.
with sync_playwright() as p:
    naive = crawl(p, False)
    polite = crawl(p, True)
print(json.dumps({"naive": naive, "polite": polite}, indent=2, ensure_ascii=False))
