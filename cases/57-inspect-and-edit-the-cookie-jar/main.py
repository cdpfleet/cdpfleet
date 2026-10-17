# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]
SITE = "https://www.httpbin.org"


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(engine, body):
    for attempt in range(1, 6):
        r = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if r.ok:
            return r.json()
        if r.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {r.status_code} {r.text}")
        time.sleep(int(r.headers.get("retry-after", 5)))


def open_browser(p, engine):
    session = launch(engine, {"proxy": PROXY, "headless": True, "record": False})
    return getattr(p, engine).connect(session["wsUrl"], headers={"x-api-key": KEY})


# httpbin answers 502/503 now and then: retry a page that isn't JSON.
def get_json(page, url):
    for attempt in range(1, 5):
        page.goto(url, timeout=45000)
        try:
            return json.loads(page.text_content("body"))
        except ValueError:
            if attempt == 4:
                raise
            time.sleep(3)


def names(cookies):
    return sorted(c["name"] for c in cookies)


with sync_playwright() as p:
    # Part 1: inspect, edit, clear and carry the jar (Chromium).
    jar = {}
    browser = open_browser(p, "chromium")
    try:
        context = browser.new_context()
        page = context.new_page()
        # The site sets two cookies (no expiry: session cookies); another site sets one more.
        get_json(page, f"{SITE}/cookies/set?session_token=abc123&theme=dark")
        context.add_cookies([{"name": "other_site", "value": "1", "url": "https://example.com"}])
        jar["all_cookies"] = names(context.cookies())
        mine = context.cookies(SITE)  # only the cookies that would be sent to this URL
        jar["for_site"] = names(mine)
        by_name = {c["name"]: c for c in mine}
        jar["session_cookie_expires"] = by_name["session_token"]["expires"]  # -1 = session cookie

        # Edit: add_cookies with the same name, domain and path replaces the cookie.
        week_from_now = int(time.time()) + 7 * 86400
        context.add_cookies([{**by_name["session_token"], "expires": week_from_now}, {**by_name["theme"], "value": "light"}])
        edited = {c["name"]: c for c in context.cookies(SITE)}
        jar["token_expires_in_days"] = round((edited["session_token"]["expires"] - time.time()) / 86400)
        jar["server_sees_after_edit"] = get_json(page, f"{SITE}/cookies")["cookies"]

        # Clear one cookie by name (other filters: domain, path).
        context.clear_cookies(name="theme")
        jar["after_clear_by_name"] = names(context.cookies())

        # Carry the login as plain JSON: just the site's cookies.
        saved = json.dumps(context.cookies(SITE), separators=(",", ":"))
        jar["saved_json_bytes"] = len(saved)
    finally:
        browser.close()

    browser = open_browser(p, "chromium")  # a new session on another browser
    try:
        context = browser.new_context()
        jar["new_session_before"] = names(context.cookies())
        context.add_cookies(json.loads(saved))
        page = context.new_page()
        jar["new_session_server_sees"] = get_json(page, f"{SITE}/cookies")["cookies"]
    finally:
        browser.close()

    # Part 2: the same three ways to set a cookie, on three hosts, in three engines.
    # httpbin.org and postman-echo.com are on the Public Suffix List; www.httpbin.org isn't.
    psl = []
    for engine in ["chromium", "firefox", "webkit"]:
        browser = open_browser(p, engine)
        try:
            for host in ["httpbin.org", "www.httpbin.org", "postman-echo.com"]:
                context = browser.new_context()
                page = context.new_page()
                url = f"https://{host}"
                context.add_cookies([{"name": "via_add_cookies", "value": "1", "url": url}])
                get_json(page, f"{url}/cookies/set?via_set_cookie=1")  # Set-Cookie, then a redirect to /cookies
                page.evaluate("() => { document.cookie = 'via_document_cookie=1; path=/; max-age=3600'; }")
                kept = names(context.cookies(url))
                psl.append({
                    "engine": engine,
                    "host": host,
                    "add_cookies": "via_add_cookies" in kept,
                    "set_cookie": "via_set_cookie" in kept,
                    "document_cookie": "via_document_cookie" in kept,
                    "server_sees": sorted(get_json(page, f"{url}/cookies")["cookies"].keys()),
                })
                context.close()
        finally:
            browser.close()

print(json.dumps({"jar": jar, "psl": psl}, indent=2))
