// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const NEXT_PROXY = process.env.SOCKS_PROXIES.split(',')[0];

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, proxy_updatable: true, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { sessionId, wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// Swap the proxy on the session's router (the host in wsUrl). Takes effect for new
// connections; the browser, its tabs, cookies and storage stay as they are.
async function swapProxy(proxy) {
  const r = await fetch(`https://${new URL(wsUrl).host}/admin/session/proxy`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId, proxy }),
  });
  if (!r.ok) throw new Error(`swap: ${r.status} ${await r.text()}`);
}

const exitIp = async (page, host) => (await (await page.goto(`https://${host}/?format=json`, { timeout: 60000 })).json()).ip;
const cookies = async (page) => (await (await page.goto('https://httpbin.org/cookies', { timeout: 60000 })).json()).cookies;

try {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto('https://httpbin.org/cookies/set?cart=3-items&login=alice', { timeout: 60000 });
  await page.evaluate(() => localStorage.setItem('draft', 'half-written review'));
  const before = { exit_ip: await exitIp(page, 'api.ipify.org'), cookies: await cookies(page) };

  const t = Date.now();
  await swapProxy(NEXT_PROXY);
  const swapMs = Date.now() - t;

  // 1. Same tab, same host: the open keep-alive connection still goes through the old proxy.
  const reusedConnection = await exitIp(page, 'api.ipify.org');
  // 2. Same tab, a host we haven't connected to yet: a new connection, so the new proxy.
  const newConnection = await exitIp(page, 'api64.ipify.org');
  // 3. Move everything over: a new context (its own connection pool) with the old
  //    cookies and localStorage.
  const moved = await browser.newContext({ storageState: await context.storageState() });
  await context.close();
  const page2 = await moved.newPage();
  const after = {
    exit_ip: await exitIp(page2, 'api.ipify.org'),
    cookies: await cookies(page2),
    local_storage: await page2.evaluate(() => localStorage.getItem('draft')),
  };
  console.log(JSON.stringify({
    before,
    swap_ms: swapMs,
    same_tab_reused_connection: reusedConnection,
    same_tab_new_connection: newConnection,
    new_context_with_storage_state: after,
    same_browser_session: browser.isConnected(),
  }, null, 2));
} finally {
  await browser.close();
}
