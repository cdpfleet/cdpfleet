# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import base64
import hashlib
import hmac
import json
import os
import re
import secrets
import struct
import time
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import urlparse

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
NAME = f"case40-python-{secrets.token_hex(4)}"  # one profile per run
PRACTICE = "https://authenticationtest.com"
SITE = "https://httpbin.org/html"
# The practice site publishes its test account and TOTP secret on the challenge page.
LOGIN = {"email": "totp@authenticationtest.com", "password": "pa$$w0rd", "secret": "I65VU7K5ZQL7WB4E"}

# Writes every kind of storage on httpbin.org, and reads it back.
WRITE_JS = """async () => {
  document.cookie = 'remember_me=alice; max-age=2592000; path=/';  // what "Remember me" sets
  document.cookie = 'session_only=yes; path=/';                    // no expiry: a session cookie
  localStorage.setItem('theme', 'dark');
  sessionStorage.setItem('draft', 'unsaved');
  await new Promise((resolve, reject) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const tx = open.result.transaction('auth', 'readwrite');
      tx.objectStore('auth').put({ email: 'alice@example.com', token: 'demo-token' }, 'user');
      tx.oncomplete = () => { open.result.close(); resolve(); };
      tx.onerror = () => reject(tx.error);
    };
  });
}"""
READ_JS = """async () => {
  const cookies = Object.fromEntries(document.cookie.split('; ').filter(Boolean).map((c) => c.split('=')));
  const idb = await new Promise((resolve) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains('auth')) { db.close(); resolve(null); return; }
      const get = db.transaction('auth').objectStore('auth').get('user');
      get.onsuccess = () => { db.close(); resolve(get.result ?? null); };
      get.onerror = () => { db.close(); resolve(null); };
    };
    open.onerror = () => resolve(null);
  });
  return {
    persistent_cookie: cookies.remember_me === 'alice',
    session_cookie: cookies.session_only === 'yes',
    local_storage: localStorage.getItem('theme') === 'dark',
    session_storage: sessionStorage.getItem('draft') === 'unsaved',
    indexeddb: idb?.email === 'alice@example.com',
  };
}"""


def totp(secret, unix_seconds):
    """RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps), as in the TOTP login case."""
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    h = hmac.new(key, struct.pack(">Q", int(unix_seconds // 30)), hashlib.sha1).digest()
    o = h[-1] & 15
    return "%06d" % ((struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000)


def api(method, path):
    res = requests.request(method, f"https://cdpfleet.com{path}", headers={"x-api-key": KEY}, timeout=30)
    return res.status_code, res.json()


def profile_info():
    return next((p for p in api("GET", "/v1/me/profiles")[1].get("profiles", []) if p["name"] == NAME), None)


def wait_for_version(version):
    """The node saves the profile after the session's browser has exited: wait for the new version."""
    for _ in range(60):
        p = profile_info()
        if p and p["version"] >= version and not p["in_use"]:
            return p
        time.sleep(2)
    raise SystemExit(f"profile {NAME} never reached version {version}")


def start(profile):
    """POST the launch with the profile (thread-safe: no Playwright here)."""
    t = time.time()
    res = requests.post("https://starter.cdpfleet.com/chromium/session", headers={"x-api-key": KEY},
                        json={"proxy": os.environ["PROXY_URL"], "headless": True, "profile": profile}, timeout=60)
    return res.status_code, res.json(), t


def attach(p, started):
    """Connect to a launched session and open a page in the profile's context."""
    status, body, t = started
    if status != 200:
        return {"status": status, "error": body.get("error")}
    browser = p.chromium.connect(body["wsUrl"], headers={"x-api-key": KEY})
    context = browser.contexts[0]  # the profile — a new_context() would not be saved
    page = context.pages[0] if context.pages else context.new_page()
    return {"status": status, "sessionId": body["sessionId"], "browser": browser, "context": context, "page": page,
            "launch_ms": round((time.time() - t) * 1000)}


def go(page, url):
    """Proxies drop a tunnel now and then: up to two retries."""
    for attempt in range(1, 4):
        try:
            return page.goto(url, timeout=60000)
        except Exception:
            if attempt == 3:
                raise
            time.sleep(2)


def storage(page):
    go(page, SITE)
    return page.evaluate(READ_JS)


def signed_into_practice(page):
    go(page, f"{PRACTICE}/")
    return page.locator("text=Sign Out").count() > 0


out = {"profile_name_prefix": "case40-", "sessions": {}, "first": {}, "saved": {}, "second": {}, "new_context": {},
       "second_writer": {}, "readonly": [], "after": {}}
with sync_playwright() as p:
    try:
        # Session A: a new profile. Log in (TOTP) and write every kind of storage.
        a = attach(p, start(NAME))
        if "browser" not in a:
            raise SystemExit(f"launch A {a['status']} {a['error']}")
        out["sessions"]["a"] = a["sessionId"]
        out["first"]["launch_ms"] = a["launch_ms"]
        try:
            go(a["page"], f"{PRACTICE}/totpChallenge/")
            if 30 - time.time() % 30 < 5:
                time.sleep(5.2)  # fresh TOTP window
            a["page"].fill("#email", LOGIN["email"])
            a["page"].fill("#password", LOGIN["password"])
            a["page"].fill("#totpmfa", totp(LOGIN["secret"], time.time()))
            a["page"].click("input[type=submit]")
            a["page"].wait_for_url(re.compile(r"/login(Success|Fail)/"), timeout=60000)
            out["first"]["practice_login"] = urlparse(a["page"].url).path == "/loginSuccess/"
            out["first"]["practice_cookie_expires"] = ["session" if c["expires"] == -1 else "persistent" for c in a["context"].cookies(PRACTICE)]
            go(a["page"], SITE)
            a["page"].evaluate(WRITE_JS)
            out["first"]["wrote"] = storage(a["page"])
        finally:
            a["browser"].close()  # ends the session; the node saves the profile once the browser has exited
        saved = wait_for_version(1)
        out["saved"] = {"version": saved["version"], "bytes": saved["bytes"]}

        # Session B: the same profile, restored.
        b = attach(p, start(NAME))
        if "browser" not in b:
            raise SystemExit(f"launch B {b['status']} {b['error']}")
        out["sessions"]["b"] = b["sessionId"]
        out["second"]["launch_ms"] = b["launch_ms"]
        try:
            out["second"]["restored"] = storage(b["page"])
            out["second"]["practice_signed_in"] = signed_into_practice(b["page"])

            # The trap: a new context in the same session starts empty and is never saved.
            scratch = b["browser"].new_context()
            out["new_context"] = storage(scratch.new_page())
            scratch.close()

            # While B holds the profile: a second normal launch is refused, read-only ones work.
            w = attach(p, start(NAME))
            out["second_writer"] = {"status": w["status"], "error": w.get("error")}
            if "browser" in w:
                w["browser"].close()
            # Launch the readers in parallel (HTTP only); the sync Playwright client is driven from this thread.
            with ThreadPoolExecutor(3) as pool:
                launches = list(pool.map(start, [{"id": NAME, "mode": "readonly"}] * 3))
            for started in launches:
                r = attach(p, started)
                if "browser" not in r:
                    out["readonly"].append({"status": r["status"], "error": r["error"]})
                    continue
                try:
                    s = storage(r["page"])
                    out["readonly"].append({"status": r["status"], "persistent_cookie": s["persistent_cookie"],
                                            "local_storage": s["local_storage"], "indexeddb": s["indexeddb"]})
                finally:
                    r["browser"].close()
            b["page"].evaluate("() => document.title")  # keep B active (idle sessions end after 60 s on shared threads)
            out["after"]["version_after_readonly"] = (profile_info() or {}).get("version")
        finally:
            b["browser"].close()
        out["after"]["version_after_b"] = wait_for_version(2)["version"]
    finally:
        # Delete the profile once no session holds it.
        for _ in range(60):
            info = profile_info()
            if not info or not info["in_use"]:
                break
            time.sleep(2)
        status, _ = api("DELETE", f"/v1/me/profiles/{NAME}")
        out["after"]["deleted"] = status == 200 and profile_info() is None
print(json.dumps(out, indent=2))
