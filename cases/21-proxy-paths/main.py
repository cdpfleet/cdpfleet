# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
# Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4).
PEET = "https://tls.peet.ws/api/all"

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, timeout=60,
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"})
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")
session = res.json()


def row(path, seen, runs_on):
    ip = seen["ip"].split(":")[0]
    # Who owns the exit: a residential ISP (your proxy) or a hosting provider (a server)?
    who = requests.get(f"http://ip-api.com/json/{ip}?fields=hosting", timeout=30).json()
    return {
        "path": path, "runs_on": runs_on, "exit_ip": ip, "exit_type": "datacenter" if who.get("hosting") else "residential",
        "user_agent": seen.get("user_agent"), "http_version": seen.get("http_version"), "ja4": seen["tls"]["ja4"],
        "tls_extensions": len(seen["tls"]["extensions"]),
        "resumed_tls": any("pre_shared_key" in (e.get("name") or "") for e in seen["tls"]["extensions"]),
    }


with sync_playwright() as p:
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        page = browser.new_page()
        # 1. A navigation: the browser itself makes the request.
        nav = page.goto(PEET, timeout=60000).json()
        # 2. fetch() inside the page (same origin): the browser's network stack, cookies and headers.
        in_page = page.evaluate("() => fetch('/api/all').then((r) => r.json())")
        # 3. page.request: Playwright's own HTTP client, run by the Playwright server next to the browser.
        via_request = page.request.get(PEET, timeout=60000).json()
        # 4. Your own HTTP client on your machine, for reference.
        local = requests.get(PEET, timeout=60).json()

        out = [
            row("page.goto", nav, "the browser"),
            row("fetch() in page.evaluate", in_page, "the browser"),
            row("page.request.get", via_request, "Playwright server"),
            row("fetch() in your script", local, "your machine"),
        ]
        # JA4's middle part hashes the cipher suites: the same TLS stack keeps it across connections.
        for r in out:
            r["same_tls_stack_as_browser"] = r["ja4"].split("_")[1] == out[0]["ja4"].split("_")[1]
        print(json.dumps(out, indent=2))
    finally:
        browser.close()
