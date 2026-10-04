# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time
from urllib.parse import parse_qsl, urlencode, urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
START = "https://quotes.toscrape.com/scroll"  # loads 10 quotes per screen, 100 in all
QUOTES_JS = "els => els.map(e => ({ text: e.querySelector('.text').textContent, author: e.querySelector('.author').textContent }))"


def by_scrolling(page):
    """Way 1: behave like a user — scroll to the bottom until nothing more appears, then read the DOM."""
    t = time.time()
    requests_ = {"n": 0}
    page.on("request", lambda _: requests_.__setitem__("n", requests_["n"] + 1))
    page.goto(START, timeout=60000)
    page.locator(".quote").first.wait_for(timeout=60000)
    loaded = time.time()
    count = scrolls = stale = 0
    while stale < 3:
        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        scrolls += 1
        page.wait_for_timeout(600)
        now = page.locator(".quote").count()
        stale = 0 if now > count else stale + 1
        count = now
    quotes = page.eval_on_selector_all(".quote", QUOTES_JS)
    done = time.time()
    return {"method": "scroll the page", "quotes": len(quotes), "authors": len({q["author"] for q in quotes}),
            "scrolls": scrolls, "requests": requests_["n"],
            "load_seconds": round(loaded - t, 3), "collect_seconds": round(done - loaded, 3)}


def by_api(page):
    """Way 2: catch the JSON request the page makes for its first screen, then call that endpoint
    yourself from inside the browser (same proxy, cookies and TLS fingerprint as the page),
    several pages at a time. No scrolling, no guessing when loading has finished."""
    t = time.time()
    requests_ = {"n": 0}
    page.on("request", lambda _: requests_.__setitem__("n", requests_["n"] + 1))
    with page.expect_response(lambda r: "/api/" in r.url and r.request.resource_type == "xhr", timeout=60000) as first:
        page.goto(START, timeout=60000)
    response = first.value
    loaded = time.time()
    endpoint = urlparse(response.url)
    params = dict(parse_qsl(endpoint.query))
    quotes = list(response.json()["quotes"])  # page 1 came for free
    wave = 4  # one round trip through the proxy per wave, not per page
    nxt = 2
    more = True
    while more:
        urls = [endpoint._replace(query=urlencode({**params, "page": str(n)})).geturl() for n in range(nxt, nxt + wave)]
        pages = page.evaluate("us => Promise.all(us.map(u => fetch(u).then(r => r.json())))", urls)
        for p in pages:
            quotes.extend(p["quotes"])
        more = all(p["has_next"] for p in pages)
        nxt += wave
    done = time.time()
    return {"method": "call its JSON API", "quotes": len(quotes), "authors": len({q["author"]["name"] for q in quotes}),
            "api_pages": nxt - 1, "endpoint": f"{endpoint.path}?page=N", "requests": requests_["n"],
            "load_seconds": round(loaded - t, 3), "collect_seconds": round(done - loaded, 3)}


res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"}, timeout=60)
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")

with sync_playwright() as p:
    browser = p.chromium.connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
    try:
        out = []
        for way in (by_scrolling, by_api):
            page = browser.new_page()  # a fresh tab per method, so request counts don't mix
            out.append(way(page))
            page.close()
        print(json.dumps(out, indent=2, ensure_ascii=False))
    finally:
        browser.close()
