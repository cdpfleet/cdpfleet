// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const API = 'https://cdpfleet.com';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Twelve light, public catalogue pages: what a crawler works through.
const URLS = Array.from({ length: 12 }, (_, i) => `https://books.toscrape.com/catalogue/page-${i + 1}.html`);

async function me() {
  const r = await fetch(`${API}/v1/me`, { headers: { 'x-api-key': KEY } });
  return (await r.json()).subscription;
}
async function launch() {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new', record: false }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const { sessionId, wsUrl } = await res.json();
  return { sessionId, browser: await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } }) };
}
// How a session ended, from the account API (it appears a few seconds after the end).
async function endReason(sessionId) {
  for (let i = 0; i < 30; i++) {
    const r = await fetch(`${API}/v1/me/sessions?limit=50`, { headers: { 'x-api-key': KEY } });
    const s = (await r.json()).sessions?.find((x) => x.session_id === sessionId);
    if (s?.ended_at) return s.end_reason;
    await sleep(3000);
  }
  return null;
}
async function load(page, url) {
  for (let attempt = 1; ; attempt++) {
    try { await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 }); return true; } catch { if (attempt === 2) return false; }
  }
}

const sub = await me();
const plan = { kind: sub.kind, max_pages: sub.features.max_pages, memory_mb: sub.features.memory_mb };

// 1 + 2: tabs. A new tab per URL, never closed, runs into the tab cap; a small pool of reused
// tabs works through the same list without ever getting near it.
const tabs = await launch();
let tabCap, pool, heapMb;
try {
  const ctx = await tabs.browser.newContext();
  const opened = [];
  let closedAt = null;
  for (let i = 0; i <= plan.max_pages; i++) {
    // Over the cap the node closes the new tab at once: newPage() fails, or the tab is already closed.
    const p = await ctx.newPage().catch(() => null);
    if (p) await sleep(1500);
    if (!p || p.isClosed()) { closedAt = i + 1; break; }
    await p.setContent(`<title>tab ${i + 1}</title>`);
    opened.push(p);
  }
  const alive = tabs.browser.isConnected() && (await opened[0].title()) === 'tab 1';
  tabCap = { opened_before_cap: opened.length, closed_at_tab: closedAt, session_alive: alive };
  for (const p of opened) await p.close();

  const POOL = 4;
  const pages = await Promise.all(Array.from({ length: POOL }, () => ctx.newPage()));
  let loaded = 0;
  for (let i = 0; i < URLS.length; i += POOL) {
    const batch = await Promise.all(URLS.slice(i, i + POOL).map((u, j) => load(pages[j], u)));
    loaded += batch.filter(Boolean).length;
  }
  // JS heap of a loaded catalogue page (Chromium's performance.memory), averaged over the pool.
  const heaps = await Promise.all(pages.map((p) => p.evaluate(() => performance.memory?.usedJSHeapSize || 0)));
  heapMb = Math.round(heaps.reduce((a, b) => a + b, 0) / heaps.length / 1048576 * 10) / 10;
  pool = { pool_size: POOL, urls: URLS.length, loaded, open_tabs: ctx.pages().length };
  for (const p of pages) await p.close();
} finally {
  await tabs.browser.close();
}

// 3: memory. Half the cap is fine; growing a page past the cap ends the session.
const mem = await launch();
let memory;
try {
  const page = await mem.browser.newPage();
  await page.evaluate(() => { window.hog = []; });
  let mb = 0, halfOk = false, cut = false;
  try {
    while (mb < plan.memory_mb + 1024) {
      await page.evaluate(() => { const a = new Uint8Array(128 << 20); for (let i = 0; i < a.length; i += 4096) a[i] = 1; window.hog.push(a); });
      mb += 128;
      if (mb >= plan.memory_mb / 2 && !halfOk) halfOk = mem.browser.isConnected();
    }
  } catch { cut = true; }
  memory = { half_cap_ok: halfOk, cut, allocated_mb_at_cut: mb };
} finally {
  await mem.browser.close().catch(() => {});
}
memory.end_reason = await endReason(mem.sessionId);

console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), plan, tab_cap: tabCap, pool, js_heap_mb_per_tab: heapMb, memory }, null, 2));
