// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';
import { readFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const ORIGIN = 'https://httpbin.org';
// Both files hold a live login: keep them somewhere safe.
const PLAIN = path.join(tmpdir(), 'cdpfleet-state.json');
const WITH_IDB = path.join(tmpdir(), 'cdpfleet-state-idb.json');

async function launch() {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
  });
  if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
  return res.json();
}

// What a Firebase Auth web app keeps after sign-in: one record in IndexedDB, nothing in
// cookies or localStorage. Plus a store of values that aren't plain JSON.
const WRITE = async () => {
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
};

// What the app finds on its next visit.
const READ = async () => {
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
};

const cookieSent = async (page) => (await (await page.goto(`${ORIGIN}/cookies`, { timeout: 60000 })).json()).cookies.session === 'abc123';
const idbOrigins = (file) => JSON.parse(readFileSync(file, 'utf8')).origins.filter((o) => o.indexedDB?.length);

// Session 1: "sign in", then save the state twice — the default way and with IndexedDB.
const s1 = await launch();
const b1 = await chromium.connect(s1.wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const ctx = await b1.newContext();
  const page = await ctx.newPage();
  await page.goto(`${ORIGIN}/cookies/set?session=abc123`, { timeout: 60000 });
  await page.evaluate(WRITE);
  await ctx.storageState({ path: PLAIN });
  await ctx.storageState({ path: WITH_IDB, indexedDB: true });
} finally {
  await b1.close();
}

// Session 2: a new browser. Restore each file into its own context and look around.
const s2 = await launch();
const b2 = await chromium.connect(s2.wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const restored = [];
  let valueTypes;
  for (const [label, file] of [['cookies + localStorage', PLAIN], ['with IndexedDB', WITH_IDB]]) {
    const ctx = await b2.newContext({ storageState: file });
    const page = await ctx.newPage();
    const cookie = await cookieSent(page);
    const seen = await page.evaluate(READ);
    restored.push({ state: label, cookie_sent: cookie, local_storage_theme: seen.local_storage_theme, firebase_user: seen.firebase_user, signed_in: seen.firebase_user !== null });
    if (file === WITH_IDB) valueTypes = seen.value_types;
    await ctx.close();
  }
  console.log(JSON.stringify({
    origin: ORIGIN,
    sessions: [s1.sessionId, s2.sessionId],
    state_files: [
      { state: 'cookies + localStorage', bytes: statSync(PLAIN).size, indexeddb_databases: idbOrigins(PLAIN).flatMap((o) => o.indexedDB.map((d) => d.name)) },
      { state: 'with IndexedDB', bytes: statSync(WITH_IDB).size, indexeddb_databases: idbOrigins(WITH_IDB).flatMap((o) => o.indexedDB.map((d) => d.name)) },
    ],
    restored,
    value_types_after_restore: valueTypes,
  }, null, 2));
} finally {
  await b2.close();
}
