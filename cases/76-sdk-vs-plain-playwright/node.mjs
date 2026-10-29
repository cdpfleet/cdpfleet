// npm install playwright@1.60.0 cdpfleet@0.1.1
// env: CDPFLEET_API_KEY, PROXY_URL, COMMUNITY_API_KEY (a Community account: 2 threads, to reach the cap)
import { chromium } from 'playwright';
import { Cdpfleet } from 'cdpfleet';

const KEY = process.env.CDPFLEET_API_KEY;
const FREE_KEY = process.env.COMMUNITY_API_KEY;
const PROXY = process.env.PROXY_URL;
const STARTER = 'https://starter.cdpfleet.com';
const API = 'https://cdpfleet.com';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const URLS = [1, 2, 3].map((n) => `https://books.toscrape.com/catalogue/page-${n}.html`);

// The scrape every variant runs: three catalogue pages, every book title.
async function scrape(browser) {
  const page = await browser.newPage();
  const titles = [];
  for (const url of URLS) {
    // A shared proxy drops the odd connection: one retry.
    await page.goto(url, { timeout: 45000 }).catch(() => page.goto(url, { timeout: 45000 }));
    titles.push(...await page.$$eval('article.product_pod h3 a', (as) => as.map((a) => a.getAttribute('title'))));
  }
  return { books: titles.length, first_title: titles[0] };
}

// ---- 1a. Plain Playwright: launch over HTTP yourself, then connect with the key header.
async function plainLaunch(key, body) {
  const res = await fetch(`${STARTER}/chromium/session`, {
    method: 'POST',
    headers: { 'x-api-key': key, 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  return { status: res.status, retryAfter: res.headers.get('retry-after'), body: await res.json().catch(() => null) };
}
async function plain() {
  const t = Date.now();
  const r = await plainLaunch(KEY, { proxy: PROXY, headless: true, record: false });
  if (r.status !== 200) throw new Error(`launch ${r.status} ${JSON.stringify(r.body)}`);
  const browser = await chromium.connect(r.body.wsUrl, { headers: { 'x-api-key': KEY } });
  const connectedMs = Date.now() - t;
  try {
    return { ...(await scrape(browser)), connected_ms: connectedMs, seconds: (Date.now() - t) / 1000 };
  } finally {
    await browser.close(); // closing the connection ends the session
  }
}

// ---- 1b. The SDK: launch() (retries 429/503 as the API asks), connect(), close().
const fleet = new Cdpfleet(); // reads CDPFLEET_API_KEY
async function withSdk() {
  const t = Date.now();
  const session = await fleet.launch('chromium', { proxy: PROXY, headless: true, record: false });
  const browser = await session.connect(chromium);
  const connectedMs = Date.now() - t;
  try {
    return { ...(await scrape(browser)), connected_ms: connectedMs, seconds: (Date.now() - t) / 1000 };
  } finally {
    await session.close();
  }
}

// ---- 1c. The SDK's connect URL: one connect() launches the browser (no HTTP call of your own).
async function withConnectUrl() {
  const t = Date.now();
  const url = fleet.connectUrl('chromium', { proxy: PROXY, headless: true, record: false });
  const browser = await chromium.connect(url, { headers: fleet.headers() });
  const connectedMs = Date.now() - t;
  try {
    return { ...(await scrape(browser)), connected_ms: connectedMs, seconds: (Date.now() - t) / 1000 };
  } finally {
    await browser.close();
  }
}

// ---- 2. At the thread cap (Community: 2 threads). Hold both, then ask for a third.
const endSession = (key, id) => fetch(`${API}/v1/me/threads/${id}`, { method: 'DELETE', headers: { 'x-api-key': key } });
async function liveThreads(key) {
  const r = await fetch(`${API}/v1/me/threads`, { headers: { 'x-api-key': key } });
  return (await r.json()).threads.length;
}
async function atTheCap() {
  const held = [];
  for (let i = 0; i < 2; i++) {
    const r = await plainLaunch(FREE_KEY, { headless: true, record: false });
    if (r.status !== 200) throw new Error(`hold ${r.status} ${JSON.stringify(r.body)}`);
    held.push(r.body.sessionId);
    await sleep(1500); // Community launches: at most 2 a second, 12 a minute
  }
  // Plain: the answer comes back at once and the retry is yours to write.
  const third = await plainLaunch(FREE_KEY, { headless: true, record: false });
  await sleep(1500);
  // SDK: the same launch waits; free a thread 5 s later and see when it returns.
  const freeFleet = new Cdpfleet({ apiKey: FREE_KEY });
  setTimeout(() => endSession(FREE_KEY, held.shift()), 5000);
  const t = Date.now();
  let retried;
  try {
    const s = await freeFleet.launch('chromium', { headless: true, record: false });
    retried = { by: 'sdk', got_session: true, waited_s: (Date.now() - t) / 1000 };
    await s.end(); // never connected: end it explicitly
  } catch (err) {
    retried = { by: 'sdk', got_session: false, waited_s: (Date.now() - t) / 1000, error: err.code };
  }
  for (const id of held) await endSession(FREE_KEY, id);
  await sleep(3000);
  return {
    threads: 2,
    plain: { status: third.status, error: third.body?.error, retry_after: third.retryAfter, in_use: third.body?.in_use, upgrade_url: third.body?.upgrade?.url ?? null },
    retry: retried,
    threads_left: await liveThreads(FREE_KEY),
  };
}

const out = { language: 'node', sdk: 'cdpfleet 0.1.1 (npm)', scrape: {} };
out.scrape.plain = await plain();
out.scrape.sdk = await withSdk();
out.scrape.connect_url = await withConnectUrl();
out.cap = await atTheCap();
out.measured_at = new Date().toISOString().slice(0, 10);
console.log(JSON.stringify(out, null, 2));
