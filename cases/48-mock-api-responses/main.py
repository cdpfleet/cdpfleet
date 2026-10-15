# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import re
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SITE = "https://shop.example.test"  # doesn't exist: every request to it is answered by the script

# A tiny shop front: loads products from its API, shows an error state, and a slideshow title
# from a real public API (httpbin.org/json).
HTML = f"""<html><head><meta charset="utf-8"></head><body><h1>Shop</h1><ul id="list"></ul><p id="status"></p><p id="title"></p>
<img src="{SITE}/banner.png"><img src="https://httpbin.org/image/png">
<script>
  fetch('/api/products').then((r) => r.ok ? r.json() : Promise.reject(r.status))
    .then((items) => {{ for (const p of items) document.getElementById('list').insertAdjacentHTML('beforeend', '<li>' + p.name + ' — ' + p.price + '</li>'); document.getElementById('status').textContent = 'ok'; }})
    .catch((code) => {{ document.getElementById('status').textContent = 'error ' + code; }});
  fetch('https://httpbin.org/json').then((r) => r.json()).then((j) => {{ document.getElementById('title').textContent = j.slideshow.title; }});
</script></body></html>"""


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(engine, body):
    for attempt in range(1, 6):
        r = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {engine} {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after") or 5))


# Open the shop with the API answering `api` ('stub' or 'error'); returns what the page shows.
def visit(context, api):
    page = context.new_page()
    aborted = 0

    def patch(route):
        response = route.fetch()
        data = response.json()
        data["slideshow"]["title"] = "Patched: " + data["slideshow"]["title"]
        route.fulfill(response=response, json=data)

    def drop(route):
        nonlocal aborted
        aborted += 1
        route.abort()

    # The page itself and its API never leave the browser (the page also declares its charset).
    page.route(f"{SITE}/", lambda route: route.fulfill(content_type="text/html; charset=utf-8", body=HTML))
    if api == "stub":
        page.route(f"{SITE}/api/products", lambda route: route.fulfill(json=[{"name": "Red mug", "price": "$9"}, {"name": "Blue mug", "price": "$11"}, {"name": "Teapot", "price": "$24"}]))
    else:
        page.route(f"{SITE}/api/products", lambda route: route.fulfill(status=500, body="boom"))
    # A real response, changed on its way in.
    page.route("https://httpbin.org/json", patch)
    # Images are dropped before they're requested.
    page.route(re.compile(r"\.png$|/image/"), drop)
    page.goto(f"{SITE}/")
    page.wait_for_function("() => document.getElementById('status').textContent && document.getElementById('title').textContent", timeout=30000)
    out = page.evaluate("""() => ({
        items: [...document.querySelectorAll('#list li')].map((li) => li.textContent),
        status: document.getElementById('status').textContent,
        title: document.getElementById('title').textContent,
    })""")
    page.close()
    return {**out, "images_aborted": aborted}


# Does the charset in the fulfilled Content-Type alone decode non-ASCII text? (No <meta>.)
def charset_header_only(context):
    page = context.new_page()
    page.route(f"{SITE}/plain", lambda route: route.fulfill(content_type="text/html; charset=utf-8", body="<p>Red mug — $9</p>"))
    page.goto(f"{SITE}/plain")
    text = page.text_content("p")
    page.close()
    return text


def one(pw, engine):
    session = launch(engine, {"proxy": os.environ["PROXY_URL"], "headless": True, "record": False})
    browser = getattr(pw, engine).connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        context = browser.new_context()
        stub = visit(context, "stub")
        error = visit(context, "error")
        header_only = charset_header_only(context)
        return {"engine": engine, "items": stub["items"], "status": stub["status"], "title": stub["title"],
                "images_aborted": stub["images_aborted"], "error_status": error["status"], "header_only_text": header_only}
    finally:
        browser.close()


with sync_playwright() as pw:
    results = [one(pw, engine) for engine in ["chromium", "firefox", "webkit"]]
print(json.dumps({"results": results}, indent=2, ensure_ascii=False))
