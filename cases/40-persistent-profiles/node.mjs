// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { createHmac, randomBytes } from 'node:crypto';
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const NAME = `case40-node-${randomBytes(4).toString('hex')}`; // one profile per run
const PRACTICE = 'https://authenticationtest.com';
const SITE = 'https://httpbin.org/html';
// The practice site publishes its test account and TOTP secret on the challenge page.
const LOGIN = { email: 'totp@authenticationtest.com', password: 'pa$$w0rd', secret: 'I65VU7K5ZQL7WB4E' };

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps), as in the TOTP login case.
function totp(secret, unixSeconds) {
  let bits = '';
  for (const c of secret) bits += 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g).map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(unixSeconds / 30)));
  const h = createHmac('sha1', key).update(counter).digest();
  const o = h[h.length - 1] & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1000000).padStart(6, '0');
}

async function api(method, path) {
  const res = await fetch(`https://cdpfleet.com${path}`, { method, headers: { 'x-api-key': KEY } });
  return { status: res.status, body: await res.json().catch(() => ({})) };
}
const profileInfo = async () => (await api('GET', '/v1/me/profiles')).body.profiles?.find((p) => p.name === NAME);
// The node saves the profile after the session's browser has exited: wait for the new version.
async function waitForVersion(version) {
  for (let i = 0; i < 60; i++) {
    const p = await profileInfo();
    if (p?.version >= version && !p.in_use) return p;
    await new Promise((r) => setTimeout(r, 2000));
  }
  throw new Error(`profile ${NAME} never reached version ${version}`);
}

// Launch with the profile. Returns the launch status/error too, so a refused launch is data.
async function launch(profile) {
  const t = Date.now();
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true, profile }),
  });
  const body = await res.json();
  if (!res.ok) return { status: res.status, error: body.error };
  const browser = await chromium.connect(body.wsUrl, { headers: { 'x-api-key': KEY } });
  const context = browser.contexts()[0]; // the profile — a newContext() would not be saved
  const page = context.pages()[0] ?? await context.newPage();
  return { status: res.status, sessionId: body.sessionId, browser, context, page, launch_ms: Date.now() - t };
}

// Proxies drop a tunnel now and then: up to two retries.
async function go(page, url) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await page.goto(url, { timeout: 60000 });
    } catch (err) {
      if (attempt === 3) throw err;
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

// What the page sees of each kind of storage on httpbin.org.
async function storage(page) {
  await go(page, SITE);
  return page.evaluate(async () => {
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
  });
}

async function signedIntoPractice(page) {
  await go(page, `${PRACTICE}/`);
  return (await page.locator('text=Sign Out').count()) > 0;
}

const out = { profile_name_prefix: 'case40-', sessions: {}, first: {}, saved: {}, second: {}, new_context: {}, second_writer: {}, readonly: [], after: {} };
try {
  // Session A: a new profile. Log in (TOTP) and write every kind of storage.
  const a = await launch(NAME);
  if (!a.browser) throw new Error(`launch A ${a.status} ${a.error}`);
  out.sessions.a = a.sessionId;
  out.first.launch_ms = a.launch_ms;
  try {
    await go(a.page, `${PRACTICE}/totpChallenge/`);
    if (30 - (Date.now() / 1000) % 30 < 5) await new Promise((r) => setTimeout(r, 5200)); // fresh TOTP window
    await a.page.fill('#email', LOGIN.email);
    await a.page.fill('#password', LOGIN.password);
    await a.page.fill('#totpmfa', totp(LOGIN.secret, Date.now() / 1000));
    await Promise.all([a.page.waitForURL(/\/login(Success|Fail)\//, { timeout: 60000 }), a.page.click('input[type=submit]')]);
    out.first.practice_login = new URL(a.page.url()).pathname === '/loginSuccess/';
    out.first.practice_cookie_expires = (await a.context.cookies(PRACTICE)).map((c) => (c.expires === -1 ? 'session' : 'persistent'));
    await go(a.page, SITE);
    await a.page.evaluate(async () => {
      document.cookie = 'remember_me=alice; max-age=2592000; path=/'; // what "Remember me" sets
      document.cookie = 'session_only=yes; path=/'; // no expiry: a session cookie
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
    });
    out.first.wrote = await storage(a.page);
  } finally {
    await a.browser.close(); // ends the session; the node saves the profile once the browser has exited
  }
  const saved = await waitForVersion(1);
  out.saved = { version: saved.version, bytes: saved.bytes };

  // Session B: the same profile, restored.
  const b = await launch(NAME);
  if (!b.browser) throw new Error(`launch B ${b.status} ${b.error}`);
  out.sessions.b = b.sessionId;
  out.second.launch_ms = b.launch_ms;
  try {
    out.second.restored = await storage(b.page);
    out.second.practice_signed_in = await signedIntoPractice(b.page);

    // The trap: a new context in the same session starts empty and is never saved.
    const scratch = await b.browser.newContext();
    out.new_context = await storage(await scratch.newPage());
    await scratch.close();

    // While B holds the profile: a second normal launch is refused, read-only ones work.
    const w = await launch(NAME);
    out.second_writer = { status: w.status, error: w.error ?? null };
    if (w.browser) await w.browser.close();
    const readers = await Promise.all([1, 2, 3].map(() => launch({ id: NAME, mode: 'readonly' })));
    for (const r of readers) {
      if (!r.browser) { out.readonly.push({ status: r.status, error: r.error }); continue; }
      try {
        const s = await storage(r.page);
        out.readonly.push({ status: r.status, persistent_cookie: s.persistent_cookie, local_storage: s.local_storage, indexeddb: s.indexeddb });
      } finally {
        await r.browser.close();
      }
    }
    await b.page.evaluate(() => document.title); // keep B active (idle sessions end after 60 s on shared threads)
    out.after.version_after_readonly = (await profileInfo())?.version;
  } finally {
    await b.browser.close();
  }
  out.after.version_after_b = (await waitForVersion(2)).version;
} finally {
  // Delete the profile once no session holds it.
  for (let i = 0; i < 60 && (await profileInfo())?.in_use; i++) await new Promise((r) => setTimeout(r, 2000));
  const del = await api('DELETE', `/v1/me/profiles/${NAME}`);
  out.after.deleted = del.status === 200 && (await profileInfo()) === undefined;
}
console.log(JSON.stringify(out, null, 2));
