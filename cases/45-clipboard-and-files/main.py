# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import time

import requests
from playwright.sync_api import TimeoutError as PlaywrightTimeoutError, sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
SETUPS = [
    {"label": "Chromium, headless", "engine": "chromium", "family": "chromium", "headless": True},
    {"label": "Chrome, headful", "engine": "chrome", "family": "chromium", "headless": False},
    {"label": "Firefox, headful", "engine": "firefox", "family": "firefox", "headless": False},
    {"label": "WebKit, headful", "engine": "webkit", "family": "webkit", "headless": False},
]

# A made-up https origin served by Playwright's router: a secure context (the clipboard and
# file APIs need one) with no network traffic. The page has a file input and a button that
# opens the File System Access picker; both report what they received in data attributes.
PAGE = "https://app.example.test/"
HTML = """<html><body>
<input type="file" id="f" onchange="document.body.dataset.input = this.files[0].name + ':' + this.files[0].size">
<button id="b" onclick="showOpenFilePicker().then(async ([h]) => { const f = await h.getFile(); document.body.dataset.picked = f.name + ':' + f.size; }).catch((e) => { document.body.dataset.picked = 'error: ' + e.name; })">open</button>
</body></html>"""

# Clipboard round trip inside the page; a permission prompt nobody answers would hang, so 3 s cap.
CLIPBOARD = """(async () => {
  const cap = (p) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('timed out (prompt?)')), 3000))]);
  const out = {};
  try { await cap(navigator.clipboard.writeText('copied-' + Date.now())); out.write = 'ok'; } catch (e) { out.write = e.name === 'Error' ? e.message : e.name; }
  try { const t = await cap(navigator.clipboard.readText()); out.read = t.startsWith('copied-') ? 'ok' : 'empty'; } catch (e) { out.read = e.name === 'Error' ? e.message : e.name; }
  return out;
})()"""

# The origin-private file system: a sandboxed folder per site, no picker, no permission.
OPFS = """(async () => {
  try {
    const root = await navigator.storage.getDirectory();
    const fh = await root.getFileHandle('note.txt', { create: true });
    const w = await fh.createWritable(); await w.write('hello opfs'); await w.close();
    return (await (await fh.getFile()).text()) === 'hello opfs' ? 'ok' : 'mismatch';
  } catch (e) { return 'no (' + e.name + ')'; }
})()"""

# Stands in for the native picker: returns a handle to a file the script provides.
PICKER_STUB = """window.showOpenFilePicker = async () => [{
  kind: 'file', name: 'report.csv', getFile: async () => new File(['id,total\\n1,42\\n'], 'report.csv', { type: 'text/csv' }),
}];"""

CSV = {"name": "report.csv", "mimeType": "text/csv", "buffer": b"id,total\n1,42\n"}


def launch(engine, body):
    # 503 means the fleet is momentarily busy: wait Retry-After and try again.
    for attempt in range(1, 6):
        res = requests.post(f"https://starter.cdpfleet.com/{engine}/session", headers={"x-api-key": KEY}, json=body, timeout=60)
        if res.ok:
            return res.json()
        if res.status_code != 503 or attempt == 5:
            raise RuntimeError(f"launch {engine} {res.status_code} {res.text}")
        time.sleep(int(res.headers.get("retry-after", 5)))


def serve(context):
    context.route(f"{PAGE}**", lambda route: route.fulfill(content_type="text/html", body=HTML))


def fresh_page(browser, init_script=None):
    context = browser.new_context()
    serve(context)
    if init_script:
        context.add_init_script(init_script)
    page = context.new_page()
    page.goto(PAGE)
    return context, page


def chooser_after_click(page, selector):
    try:
        with page.expect_file_chooser(timeout=3000) as info:
            page.click(selector)
        return info.value
    except PlaywrightTimeoutError:
        return None


def one(p, s):
    session = launch(s["engine"], {"proxy": os.environ["PROXY_URL"], "headless": s["headless"]})
    browser = getattr(p, s["family"]).connect(session["wsUrl"], headers={"x-api-key": KEY})
    out = {"label": s["label"]}
    try:
        # 1. Clipboard with no permissions granted.
        context, page = fresh_page(browser)
        d = page.evaluate(CLIPBOARD)
        out["clipboard_write_default"], out["clipboard_read_default"] = d["write"], d["read"]

        # 2. Files in the same page: <input type=file>, the File System Access picker, the private file system.
        fc = chooser_after_click(page, "#f")
        if fc:
            fc.set_files(CSV)
        out["input_set_files"] = page.evaluate("() => document.body.dataset.input || 'nothing'") if fc else "no filechooser event"
        out["show_open_file_picker"] = page.evaluate("() => typeof window.showOpenFilePicker === 'function'")
        out["picker_filechooser_event"] = chooser_after_click(page, "#b") is not None if out["show_open_file_picker"] else False
        out["private_file_system"] = page.evaluate(OPFS)
        context.close()

        # 3. The picker stubbed with an init script: the page's own code receives our file.
        context, page = fresh_page(browser, PICKER_STUB)
        page.click("#b")
        page.wait_for_function("() => document.body.dataset.picked", timeout=5000)
        out["picker_stubbed"] = page.evaluate("() => document.body.dataset.picked")
        context.close()

        # 4. Clipboard with Chromium's permission names granted. Some engines reject the names —
        # at grant_permissions or when the next page opens.
        context = browser.new_context()
        serve(context)
        try:
            context.grant_permissions(["clipboard-read", "clipboard-write"], origin=PAGE)
            page = context.new_page()
            page.goto(PAGE)
            out["grant_clipboard"] = "accepted"
            g = page.evaluate(CLIPBOARD)
            out["clipboard_write_granted"], out["clipboard_read_granted"] = g["write"], g["read"]
        except Exception as err:
            msg = str(err).split("\n")[0]
            out["grant_clipboard"] = msg.split(": ", 1)[1][:80] if ": " in msg else msg[:80]
    finally:
        browser.close()
    return out


with sync_playwright() as p:
    results = [one(p, s) for s in SETUPS]
print(json.dumps({"results": results}, indent=2))
