# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one page load per test page per setup)
import json
import os
import time

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
PROXY = os.environ["PROXY_URL"]

SETUPS = [
    {"setup": "Chromium, headless", "engine": "chromium", "family": "chromium", "body": {"headless": "new"}},
    {"setup": "Google Chrome, headful", "engine": "chrome", "family": "chromium", "body": {"headless": False}},
    {"setup": "Patchright, headful", "engine": "patchright", "family": "chromium", "body": {"headless": False}},
    {"setup": "Camoufox, defaults", "engine": "camoufox", "family": "firefox", "body": {"headless": True}},
    {"setup": "WebKit, headless", "engine": "webkit", "family": "webkit", "body": {"headless": True}},
]

# Each test page's own verdict, read from its DOM. The same expressions run in every language;
# "ready" is polled until the page has finished its checks.
PAGES = [
    {
        "key": "sannysoft", "url": "https://bot.sannysoft.com/",
        "ready": r"""document.querySelectorAll('td.passed, td.failed').length > 10""",
        "read": r"""(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()""",
    },
    {
        "key": "creepjs", "url": "https://abrahamjuliot.github.io/creepjs/",
        "ready": r"""/\d+% stealth/.test(document.body.innerText)""",
        "read": r"""(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const m = /(\d+)% like headless[\s\S]*?(\d+)% headless:[\s\S]*?(\d+)% stealth/.exec(t); return m ? { like_headless_pct: +m[1], headless_pct: +m[2], stealth_pct: +m[3] } : { like_headless_pct: null, headless_pct: null, stealth_pct: null }; })()""",
    },
    {
        "key": "deviceandbrowserinfo", "url": "https://deviceandbrowserinfo.com/are_you_a_bot",
        "ready": r"""document.body.innerText.indexOf('"isBot"') >= 0""",
        "read": r"""(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('"isBot"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()""",
    },
    {
        "key": "fingerprint", "url": "https://demo.fingerprint.com/playground",
        "ready": r"""/Suspect Score \d+/.test(document.body.innerText.replace(/\s+/g, ' '))""",
        # Only the bot verdict and the suspect score are read (the page also names proxy networks).
        "read": r"""(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const bot = /\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\x20-\x7e]/g, '').replace(/\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()""",
    },
]


# 503 means the fleet is momentarily busy: wait Retry-After and try again.
def launch(engine, body):
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY},
                            json={"proxy": PROXY, "record": False, **body}, timeout=120)
        if res.ok:
            return res.json()
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


results = []
with sync_playwright() as p:
    for s in SETUPS:
        session = launch(s["engine"], s["body"])
        browser = getattr(p, s["family"]).connect(session["wsUrl"], headers={"x-api-key": KEY})
        row = {"setup": s["setup"], "engine": s["engine"]}
        try:
            page = browser.new_context().new_page()
            for pg in PAGES:
                try:
                    page.goto(pg["url"], timeout=60000)
                    page.wait_for_function(pg["ready"], timeout=45000)
                    row[pg["key"]] = page.evaluate(pg["read"])
                except Exception as err:
                    row[pg["key"]] = {"error": str(err).split("\n")[0][:120]}
        finally:
            browser.close()
        results.append(row)
print(json.dumps(results, indent=2))
