# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]

# Three visitors who must not see each other's state — in ONE browser session (1 thread).
PERSONAS = [
    {"name": "alice", "locale": "en-US", "timezone": "America/New_York"},
    {"name": "bruno", "locale": "pt-BR", "timezone": "America/Sao_Paulo"},
    {"name": "chie", "locale": "ja-JP", "timezone": "Asia/Tokyo"},
]

SEEN = """() => ({
  cookie: document.cookie,
  storage_owner: localStorage.getItem('owner'),
  language: navigator.language,
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  clock: new Date('2026-10-05T12:00:00Z').toLocaleTimeString(),
})"""

res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY}, timeout=60,
                    json={"proxy": os.environ["PROXY_URL"], "headless": "new"})
if not res.ok:
    raise SystemExit(f"launch {res.status_code} {res.text}")
session = res.json()

with sync_playwright() as p:
    browser = p.chromium.connect(session["wsUrl"], headers={"x-api-key": KEY})
    try:
        pages = []
        for persona in PERSONAS:
            # Each context is a separate profile: its own cookies, storage, locale and clock.
            context = browser.new_context(locale=persona["locale"], timezone_id=persona["timezone"])
            context.add_cookies([{"name": "session", "value": f"{persona['name']}-token", "domain": "example.com", "path": "/"}])
            page = context.new_page()
            page.goto("https://example.com/", timeout=60000)
            page.evaluate("(n) => localStorage.setItem('owner', n)", persona["name"])
            pages.append((persona, page))
        out = []
        for persona, page in pages:
            # Read everything after all three exist, so any leak between them would show.
            seen = page.evaluate(SEEN)
            ip = page.goto("http://ip-api.com/json/?fields=query", timeout=60000).json()
            out.append({"persona": persona["name"], "threads": session["weight"], **seen, "exit_ip": ip["query"]})
        print(json.dumps(out, indent=2, ensure_ascii=False))
    finally:
        browser.close()
