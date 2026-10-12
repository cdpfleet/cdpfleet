# pip install playwright==1.60.0 requests
# env: CDPFLEET_API_KEY, PROXY_URL
import json
import os
import tempfile

import requests
from playwright.sync_api import sync_playwright

KEY = os.environ["CDPFLEET_API_KEY"]
ORIGIN = "https://httpbin.org"
# Both files hold a live login: keep them somewhere safe.
PLAIN = os.path.join(tempfile.gettempdir(), "cdpfleet-state.json")
WITH_IDB = os.path.join(tempfile.gettempdir(), "cdpfleet-state-idb.json")


def launch():
    r = requests.post(
        "https://starter.cdpfleet.com/chromium/session",
        headers={"x-api-key": KEY},
        json={"proxy": os.environ["PROXY_URL"], "headless": True},
        timeout=90,
    )
    if not r.ok:
        raise RuntimeError(f"launch: {r.status_code} {r.text}")
    return r.json()


# What a Firebase Auth web app keeps after sign-in: one record in IndexedDB, nothing in
# cookies or localStorage. Plus a store of values that aren't plain JSON.
WRITE = """async () => {
  const open = (name, setup) => new Promise((ok, no) => {
    const r = indexedDB.open(name, 1);
    r.onupgradeneeded = () => setup(r.result);
    r.onsuccess = () => ok(r.result);
    r.onerror = () => no(r.error);
  });
  const put = (db, store, value, key) => new Promise((ok, no) => {
    const tx = db.transaction(store, 'readwrite');
    tx.objectStore(store).put(value, key);
    tx.oncomplete = ok;
    tx.onerror = () => no(tx.error);
  });
  const fb = await open('firebaseLocalStorageDb', (db) => db.createObjectStore('firebaseLocalStorage', { keyPath: 'fbase_key' }));
  await put(fb, 'firebaseLocalStorage', {
    fbase_key: 'firebase:authUser:AIzaDemoKey:[DEFAULT]',
    value: { uid: 'u_8f3a', email: 'alice@example.com', stsTokenManager: { refreshToken: 'demo-refresh-token', accessToken: 'demo-access-token', expirationTime: Date.now() + 3600000 } },
  });
  const types = await open('typesDb', (db) => db.createObjectStore('values'));
  const values = { date: new Date(0), bytes: new Uint8Array([1, 2, 3]), bigint: 10n, map: new Map([['k', 1]]), blob: new Blob(['hi']) };
  for (const [k, v] of Object.entries(values)) await put(types, 'values', v, k);
  localStorage.setItem('theme', 'dark');
}"""

# What the app finds on its next visit.
READ = """async () => {
  const dbs = (await indexedDB.databases()).map((d) => d.name);
  const get = (name, store, key) => new Promise((ok) => {
    if (!dbs.includes(name)) return ok(undefined);
    const r = indexedDB.open(name);
    r.onsuccess = () => {
      const q = r.result.transaction(store).objectStore(store).get(key);
      q.onsuccess = () => ok(q.result);
      q.onerror = () => ok(undefined);
    };
    r.onerror = () => ok(undefined);
  });
  const user = await get('firebaseLocalStorageDb', 'firebaseLocalStorage', 'firebase:authUser:AIzaDemoKey:[DEFAULT]');
  const types = {};
  for (const k of ['date', 'bytes', 'bigint', 'map', 'blob']) {
    const v = await get('typesDb', 'values', k);
    const t = v === undefined ? 'missing' : typeof v === 'bigint' ? 'BigInt' : Object.prototype.toString.call(v).slice(8, -1);
    types[k] = t === 'Object' && Object.keys(v).length === 0 ? 'Object {} (emptied)' : t;
  }
  return { local_storage_theme: localStorage.getItem('theme'), firebase_user: user ? user.value.email : null, value_types: types };
}"""


def cookie_sent(page):
    return page.goto(f"{ORIGIN}/cookies", timeout=60000).json()["cookies"].get("session") == "abc123"


def idb_databases(file):
    with open(file) as f:
        origins = json.load(f)["origins"]
    return [d["name"] for o in origins for d in o.get("indexedDB") or []]


with sync_playwright() as p:
    # Session 1: "sign in", then save the state twice: the default way and with IndexedDB.
    s1 = launch()
    b1 = p.chromium.connect(s1["wsUrl"], headers={"x-api-key": KEY})
    try:
        ctx = b1.new_context()
        page = ctx.new_page()
        page.goto(f"{ORIGIN}/cookies/set?session=abc123", timeout=60000)
        page.evaluate(WRITE)
        ctx.storage_state(path=PLAIN)
        ctx.storage_state(path=WITH_IDB, indexed_db=True)
    finally:
        b1.close()

    # Session 2: a new browser. Restore each file into its own context and look around.
    s2 = launch()
    b2 = p.chromium.connect(s2["wsUrl"], headers={"x-api-key": KEY})
    try:
        restored, value_types = [], None
        for label, file in [("cookies + localStorage", PLAIN), ("with IndexedDB", WITH_IDB)]:
            ctx = b2.new_context(storage_state=file)
            page = ctx.new_page()
            cookie = cookie_sent(page)
            seen = page.evaluate(READ)
            restored.append({
                "state": label,
                "cookie_sent": cookie,
                "local_storage_theme": seen["local_storage_theme"],
                "firebase_user": seen["firebase_user"],
                "signed_in": seen["firebase_user"] is not None,
            })
            if file == WITH_IDB:
                value_types = seen["value_types"]
            ctx.close()
        print(json.dumps({
            "origin": ORIGIN,
            "sessions": [s1["sessionId"], s2["sessionId"]],
            "state_files": [
                {"state": "cookies + localStorage", "bytes": os.path.getsize(PLAIN), "indexeddb_databases": idb_databases(PLAIN)},
                {"state": "with IndexedDB", "bytes": os.path.getsize(WITH_IDB), "indexeddb_databases": idb_databases(WITH_IDB)},
            ],
            "restored": restored,
            "value_types_after_restore": value_types,
        }, indent=2))
    finally:
        b2.close()
