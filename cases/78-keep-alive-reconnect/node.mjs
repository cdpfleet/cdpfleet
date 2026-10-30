// npm install playwright@1.60.0
// env: CDPFLEET_DEDICATED_API_KEY (keep_alive needs dedicated threads), PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_DEDICATED_API_KEY;
const PROXY = process.env.PROXY_URL;
const STARTER = 'https://starter.cdpfleet.com';
const API = 'https://cdpfleet.com';
const IDLE_S = 30; // inactivity_timeout: how long the session waits for a client
const SITE = 'https://books.toscrape.com';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const secs = (a, b) => Math.round((b - a) / 100) / 10;
const headers = { 'x-api-key': KEY };
const api = (path) => fetch(`${API}${path}`, { headers }).then((r) => r.json());
// The session as the account API sees it while it runs: listed in /v1/me/threads or not.
const listed = async (id) => (await api('/v1/me/threads')).threads.find((t) => t.session_id === id) || null;
// A shared proxy drops the odd connection: one retry.
const goto = (page, url) => page.goto(url, { timeout: 45000 }).catch(() => page.goto(url, { timeout: 45000 }));

// Today's thread-seconds on the account, closed and still running.
const usage = async () => { const u = await api('/v1/me/usage'); return { closed: u.closed_thread_seconds, live: u.live_thread_seconds }; };
const usageBefore = await usage();

// 1. Launch with keep_alive: a disconnect no longer ends the session.
const t0 = Date.now();
const res = await fetch(`${STARTER}/chromium/session`, {
  method: 'POST',
  headers: { ...headers, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: PROXY, headless: 'new', keep_alive: true, inactivity_timeout: `${IDLE_S}s` }),
});
if (!res.ok) throw new Error(`launch failed: ${res.status} ${await res.text()}`);
const { sessionId, wsUrl } = await res.json();

// 2. First client: an agent works through the catalogue in one context, a second context on the side.
let browser = await chromium.connect(wsUrl, { headers });
const work = await browser.newContext();
const page = await work.newPage();
await goto(page, `${SITE}/catalogue/page-1.html`);
await page.click('li.next a'); // step 2 of the run: page 2
await work.addCookies([{ name: 'agent_step', value: '2', domain: 'books.toscrape.com', path: '/' }]);
await page.evaluate(() => localStorage.setItem('agent_state', JSON.stringify({ seen: 40, next: 'page-3' })));
const detail = await work.newPage();
await goto(detail, `${SITE}/catalogue/a-light-in-the-attic_1000/index.html`);
const side = await browser.newContext();
await goto(await side.newPage(), `${SITE}/catalogue/category/books/poetry_23/index.html`);
const before = { contexts: browser.contexts().length, pages: browser.contexts().reduce((n, c) => n + c.pages().length, 0), url: page.url() };

// 3. The client goes away. With keep_alive, close() only disconnects.
await browser.close();
const tDetach = Date.now();
await sleep(3000);
const whileDetached = await listed(sessionId);
await sleep(12000);

// 4. A new client reconnects to the same wsUrl and carries on where the first one stopped.
const tReconnect = Date.now();
browser = await chromium.connect(wsUrl, { headers });
const reconnectMs = Date.now() - tReconnect;
const detachedS = secs(tDetach, tReconnect);
const contexts = browser.contexts();
const all = contexts.flatMap((c) => c.pages().map((p) => ({ c, p })));
const back = all.find(({ p }) => p.url().includes('page-2.html'));
const after = {
  contexts: contexts.length,
  pages: all.length,
  urls: all.map(({ p }) => p.url().replace(SITE, '')).sort(),
  cookie: back ? (await back.c.cookies(SITE)).find((c) => c.name === 'agent_step')?.value ?? null : null,
  local_storage: back ? await back.p.evaluate(() => localStorage.getItem('agent_state')) : null,
};
let continued = null;
if (back) {
  await back.p.click('li.next a');
  await back.p.waitForURL('**/page-3.html');
  continued = back.p.url().replace(SITE, '');
}

// 5. The client leaves for good: the session waits inactivity_timeout, then ends.
await browser.close();
const tLeave = Date.now();
let goneAfterS = null;
let usageWaiting = null;
while (Date.now() - tLeave < (IDLE_S + 90) * 1000) {
  await sleep(2000);
  if (!usageWaiting && Date.now() - tLeave >= 20000) usageWaiting = await usage();
  if (!(await listed(sessionId))) { goneAfterS = secs(tLeave, Date.now()); break; }
}
let lateReconnect;
try {
  const b = await chromium.connect(wsUrl, { headers, timeout: 20000 });
  lateReconnect = { connected: true, contexts: b.contexts().length };
  await b.close();
} catch (err) {
  lateReconnect = { connected: false, error: err.message.split('\n')[0].slice(0, 160) };
}

// 6. What was billed: the session's record in the account history.
await sleep(5000);
const { sessions } = await api('/v1/me/sessions?limit=20');
const s = sessions.find((x) => x.session_id === sessionId);
const usageAfter = await usage();

console.log(JSON.stringify({
  language: 'node',
  inactivity_timeout_s: IDLE_S,
  before,
  detached: {
    listed: Boolean(whileDetached),
    running_seconds: whileDetached?.running_seconds ?? null,
    seconds: detachedS,
  },
  reconnect: { ms: reconnectMs, ...after, continued },
  end: {
    gone_after_s: goneAfterS,
    late_reconnect: lateReconnect,
    end_reason: s?.end_reason ?? null,
    duration_seconds: s?.duration_seconds ?? null,
    weight: s?.weight ?? null,
    client_seconds: secs(t0, tLeave),
  },
  usage: {
    live_while_waiting: usageWaiting ? usageWaiting.live : null,
    billed: usageAfter.closed - usageBefore.closed,
  },
  measured_at: new Date().toISOString().slice(0, 10),
}, null, 2));
