// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { createHmac } from 'node:crypto';
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITE = 'https://authenticationtest.com';
// The practice site publishes its test account and TOTP secret on the challenge page.
const LOGIN = { email: 'totp@authenticationtest.com', password: 'pa$$w0rd', secret: 'I65VU7K5ZQL7WB4E' };

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps) from the base32 secret — what an authenticator app does.
function totp(secret, unixSeconds) {
  let bits = '';
  for (const c of secret.replace(/=+$/, '')) bits += 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(c).toString(2).padStart(5, '0');
  const key = Buffer.from(bits.match(/.{8}/g).map((b) => parseInt(b, 2)));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(unixSeconds / 30)));
  const h = createHmac('sha1', key).update(counter).digest();
  const o = h[h.length - 1] & 15;
  return String((h.readUInt32BE(o) & 0x7fffffff) % 1000000).padStart(6, '0');
}

// A code typed in the last seconds of its window can expire before the server checks it:
// wait for the next window when fewer than 5 s are left.
async function freshWindow() {
  const left = 30 - (Date.now() / 1000) % 30;
  if (left < 5) await new Promise((r) => setTimeout(r, left * 1000 + 200));
  return { seconds_left_in_window: Math.round(left * 10) / 10, waited_s: left < 5 ? Math.round(left * 10) / 10 : 0 };
}

async function launch() {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const { sessionId, wsUrl } = await res.json();
  return { sessionId, browser: await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } }) };
}

// One login attempt in a fresh context (no cookies from earlier attempts). The code is made
// after the form has loaded, right before typing it: offset null types a wrong code, otherwise
// the code for now + offset seconds.
async function attemptOnce(browser, label, offset) {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.goto(`${SITE}/totpChallenge/`, { timeout: 60000 });
  const window = await freshWindow();
  const code = offset === null ? '000000' : totp(LOGIN.secret, Date.now() / 1000 + offset);
  const t = Date.now();
  await page.fill('#email', LOGIN.email);
  await page.fill('#password', LOGIN.password);
  await page.fill('#totpmfa', code);
  await Promise.all([page.waitForURL(/\/login(Success|Fail)\//, { timeout: 60000 }), page.click('input[type=submit]')]);
  const landed = new URL(page.url()).pathname;
  return { context, result: { attempt: label, offset_s: offset, ...window, landed, accepted: landed === '/loginSuccess/', ms: Date.now() - t } };
}

// The challenge page or the post-submit navigation occasionally hangs through a rotating proxy:
// one more try in a fresh context, with the code made again after the reload.
async function attempt(browser, label, offset) {
  try { return await attemptOnce(browser, label, offset); } catch (err) {
    if (err.name !== 'TimeoutError') throw err;
    for (const c of browser.contexts()) await c.close().catch(() => {});
    return attemptOnce(browser, label, offset);
  }
}

const signedIn = async (page) => (await page.locator('text=Sign Out').count()) > 0;

const out = { site: `${SITE}/totpChallenge/`, session_ids: {}, clocks: {}, attempts: [], storage_state: null, second_session: null };
let state;
const first = await launch();
out.session_ids.first = first.sessionId;
try {
  // What matters is your clock against the server's. The site's Date header (from its CDN
  // edge, 1 s resolution) should fall between our clock just before and just after the
  // request. The remote browser's clock is recorded for comparison only.
  const page = await first.browser.newPage();
  const t0 = Date.now() / 1000;
  const resp = await page.goto(`${SITE}/totpChallenge/`, { timeout: 60000 });
  const t1 = Date.now() / 1000;
  const server = Date.parse(resp.headers().date) / 1000;
  out.clocks.server_date_within_request = server >= Math.floor(t0) - 1 && server <= t1 + 1;
  out.clocks.request_s = Math.round((t1 - t0) * 10) / 10;
  out.clocks.browser_minus_local_s = Math.round((await page.evaluate(() => Date.now()) - Date.now()) / 100) / 10;
  await page.close();

  for (const [label, offset] of [
    ['wrong code', null],
    ['code from 2 windows ago (-60 s)', -60],
    ['code from the previous window (-30 s)', -30],
    ['code from the next window (+30 s)', 30],
  ]) {
    const { context, result } = await attempt(first.browser, label, offset);
    out.attempts.push(result);
    await context.close();
  }
  const { context, result } = await attempt(first.browser, 'current code', 0);
  out.attempts.push(result);
  state = await context.storageState();
  out.storage_state = { cookies: state.cookies.length, session_cookies: state.cookies.filter((c) => c.expires === -1).length };
} finally {
  await first.browser.close();
}

// A new session (another browser): without the saved state, then with it.
const second = await launch();
out.session_ids.second = second.sessionId;
try {
  out.second_session = {};
  for (const [label, options] of [['without_state', {}], ['with_state', { storageState: state }]]) {
    const context = await second.browser.newContext(options);
    const page = await context.newPage();
    await page.goto(`${SITE}/`, { timeout: 60000 });
    out.second_session[label] = { signed_in: await signedIn(page) };
    await context.close();
  }
} finally {
  await second.browser.close();
}
console.log(JSON.stringify(out, null, 2));
