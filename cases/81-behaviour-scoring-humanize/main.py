# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY (one session at a time; public test pages only; nothing is solved or retried to get a result)
import json
import os
import secrets
import time
from datetime import datetime, timezone

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
# One sticky residential exit for the whole run, shared by every setup: a difference between rows is the browser
# and how it was driven, not the IP.
PROXY = f"cdpfleet-resi-country-us-session-c81{secrets.token_hex(3)}-lifetime-30"
REPEATS = 3
# Camoufox's humanize option: False = the cursor jumps straight to the target, True (the default) = a curved path of
# up to 1.5 s, a number = a path of up to that many seconds. Chrome is the reference: plain Playwright clicks.
SETUPS = [
    {"key": "cf_off", "setup": "Camoufox, humanize: false", "engine": "camoufox", "type": "firefox", "body": {"headless": True, "humanize": False}},
    {"key": "cf_on", "setup": "Camoufox, humanize: true (default)", "engine": "camoufox", "type": "firefox", "body": {"headless": True, "humanize": True}},
    {"key": "cf_slow", "setup": "Camoufox, humanize: 3", "engine": "camoufox", "type": "firefox", "body": {"headless": True, "humanize": 3}},
    {"key": "chrome", "setup": "Google Chrome, headful", "engine": "chrome", "type": "chromium", "body": {"headless": False}},
]
# The same actions in every cell; only the typing speed differs between the two form runs of a session.
TYPING = {"fast": 0, "paced": 120}  # ms between keys
EMAIL = "lab81@example.com"
PASSWORD = "lab-81!pass"  # the demo form wants 8+ characters with a special character

# deviceandbrowserinfo.com's behavioural bot test: a demo login form that records clicks, keystrokes and timing
# and, on submit, posts them with a fingerprint to its own endpoint, which answers human or bot with its flags.
FORM = "https://deviceandbrowserinfo.com/are_you_a_bot_interactions"
# Google's public reCAPTCHA v3 demo prints its server's verify response (with the score); its "Try again" link reloads it.
RECAPTCHA = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php"

# The same expressions run in every language.
# Count mousemove events on the page, so the table shows what humanize changed.
COUNT_MOVES = "(() => { window.__lab81 = 0; document.addEventListener('mousemove', () => { window.__lab81++; }, true); return true; })()"
# The verdict the page shows after submit: isBot and every detail flag that is true.
VERDICT = """(() => {
  const e = document.getElementById('jsonResult');
  const t = e ? e.innerText : '';
  if (t.length < 10) return null;
  const r = JSON.parse(t);
  const d = r.details || {};
  return { is_bot: r.isBot === true, flags: Object.keys(d).filter((k) => d[k] === true), moves: window.__lab81 };
})()"""
# What the page itself measured about the interaction: the "interactions" part of the body it posted, which is
# embedded as a JSON string and passed to this function.
MEASURED = """(body) => {
  const i = JSON.parse(body).interactions || {};
  const s = i.emailTimingStats || {};
  return {
    time_to_fill_ms: Math.round(i.timeToFill),
    exact_center_clicks: [i.hasClickedEmailFieldExactCenter, i.hasClickedPasswordFieldExactCenter, i.hasClickedSubmitExactCenter].filter(Boolean).length,
    untrusted_event: i.hasUntrustedEvent === true,
    cdp_mouse_leak: i.cdpMouseLeak === true,
    key_gap_median_ms: typeof s.medianTimeBetweenKeys === 'number' ? Math.round(s.medianTimeBetweenKeys) : null,
  };
}"""
SCORE = r"""(() => { if (window.__lab81rc) return null; const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()"""


def launch(p, s):
    body = {"proxy": PROXY, "record": False, **s["body"]}
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{s['engine']}/session", headers={"x-api-key": KEY}, json=body, timeout=120)
        if res.ok:
            return getattr(p, s["type"]).connect(res.json()["wsUrl"], headers={"x-api-key": KEY})
        if res.status_code not in (429, 503) or attempt == 5:
            raise RuntimeError(f"launch {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after") or 5))


def safe(tab, expr):
    try:
        return tab.evaluate(expr)
    except Exception:
        return None


# A load that fails on the network (proxy timeout) is tried once more; the page's answer is never retried.
def open_page(tab, url):
    for _ in range(2):
        try:
            tab.goto(url, wait_until="domcontentloaded", timeout=30000)
            return True
        except Exception:
            pass
    return False


# One form run: click the email field, type, click the password field, type, click Login; read the verdict.
def form(browser, typing):
    out = {"typing": typing, "verdict": "error", "flags": [], "moves": None, "action_seconds": None, "measured": None}
    context = browser.new_context()  # a fresh context: every run is a first visit
    try:
        tab = context.new_page()
        posted = {}

        def on_request(req):
            if "/fingerprint_bot_test" in req.url:
                posted["body"] = req.post_data

        tab.on("request", on_request)
        if not open_page(tab, FORM):
            return out
        time.sleep(1)
        tab.evaluate(COUNT_MOVES)
        t0 = time.time()
        tab.click("#email")
        tab.keyboard.type(EMAIL, delay=TYPING[typing])
        tab.click("#password")
        tab.keyboard.type(PASSWORD, delay=TYPING[typing])
        tab.click("#loginForm button[type=submit]")
        out["action_seconds"] = round((time.time() - t0) * 10) / 10
        v = None
        for _ in range(60):
            time.sleep(0.5)
            v = safe(tab, VERDICT)
            if v is not None:
                break
        if v is None:
            out["verdict"] = "no verdict"
            return out
        out.update({"verdict": "bot" if v["is_bot"] else "human", "flags": v["flags"], "moves": v["moves"]})
        if posted.get("body"):
            out["measured"] = tab.evaluate(f"({MEASURED})({json.dumps(posted['body'])})")
        return out
    except Exception:
        return out
    finally:
        try:
            context.close()
        except Exception:
            pass


# Google's v3 demo: wait for the score it asks for on load, move the cursor and click "Try again", read the new score.
def recaptcha(browser):
    out = {"before": None, "after": None}
    context = browser.new_context()
    try:
        tab = context.new_page()
        if not open_page(tab, RECAPTCHA):
            return out
        for _ in range(40):
            time.sleep(0.5)
            out["before"] = safe(tab, SCORE)
            if out["before"] is not None:
                break
        tab.evaluate("window.__lab81rc = 1")  # marks this document, so the score is read from the reloaded one
        tab.mouse.move(300, 300)
        tab.mouse.move(600, 200)
        tab.click('a[href="/recaptcha-v3-request-scores.php"]')
        for _ in range(40):
            time.sleep(0.5)
            out["after"] = safe(tab, SCORE)
            if out["after"] is not None:
                break
        return out
    except Exception:
        return out
    finally:
        try:
            context.close()
        except Exception:
            pass


runs = []
scores = []
with sync_playwright() as p:
    # Serial: one session at a time; the repeats are interleaved so a slow minute doesn't land on one setup.
    for repeat in range(1, REPEATS + 1):
        for s in SETUPS:
            base = {"setup": s["setup"], "key": s["key"], "repeat": repeat}
            try:
                browser = launch(p, s)
            except Exception:
                for typing in TYPING:
                    runs.append({**base, "typing": typing, "verdict": "error", "flags": [], "moves": None, "action_seconds": None, "measured": None})
                scores.append({**base, "before": None, "after": None})
                continue
            for typing in TYPING:
                runs.append({**base, **form(browser, typing)})
            scores.append({**base, **recaptcha(browser)})
            try:
                browser.close()
            except Exception:
                pass

print(json.dumps({"measured_at": datetime.now(timezone.utc).strftime("%Y-%m-%d"), "runs": runs, "recaptcha": scores}, indent=2))
