// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import { randomBytes } from 'node:crypto';
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// The sticky residential exit is shared by every setup, so a difference between rows is the browser.
const NETWORKS = {
  own: { label: 'own proxy (rotating)', proxy: process.env.PROXY_URL },
  tier1: { label: 'cdpfleet-resi Tier 1, sticky US', proxy: `cdpfleet-resi-country-us-session-c68${RUN}-lifetime-30` },
};
const SETUPS = {
  chromium: { setup: 'Chromium, headless', engine: 'chromium', type: chromium, headless: true },
  chrome: { setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, headless: false },
  edge: { setup: 'Microsoft Edge, headful', engine: 'edge', type: chromium, headless: false },
  brave: { setup: 'Brave, headful', engine: 'brave', type: chromium, headless: false },
  camoufox: { setup: 'Camoufox, defaults', engine: 'camoufox', type: firefox, headless: true },
  firefox: { setup: 'Firefox, headful', engine: 'firefox', type: firefox, headless: false },
  webkit: { setup: 'WebKit, headful', engine: 'webkit', type: webkit, headless: false },
};
// Vendors' own public demo pages. Google's v3 demo asks for a score as soon as it loads and prints its
// server's verify response (with the score) on the page; hCaptcha's demo shows a checkbox widget.
const RECAPTCHA = 'https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php';
const HCAPTCHA = 'https://accounts.hcaptcha.com/demo';

// Read what the page shows; the same expressions run in every language.
const SCORE = `(() => { const m = /"score"\\s*:\\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()`;
const HC_WIDGET = `(() => { const f = document.querySelector('iframe[src*="hcaptcha.com"]'); return f ? (f.getBoundingClientRect().height > 0) : false; })()`;

async function launch(s, network) {
  const body = { proxy: NETWORKS[network].proxy, headless: s.headless, record: false };
  // WebKit takes an authenticated HTTP proxy through the updatable route (see the WebKit page).
  if (s.engine === 'webkit' && /^https?:/.test(body.proxy)) body.proxy_updatable = true;
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${s.engine}/session`, {
      method: 'POST', headers: { 'x-api-key': KEY, 'content-type': 'application/json' }, body: JSON.stringify(body),
    });
    if (res.ok) return s.type.connect((await res.json()).wsUrl, { headers: { 'x-api-key': KEY } });
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// One load; then wait up to 20 s for the score the demo prints. Nothing on the page is touched.
async function recaptcha(context) {
  const tab = await context.newPage();
  try {
    await tab.goto(RECAPTCHA, { waitUntil: 'domcontentloaded', timeout: 45000 });
  } catch {
    await tab.close();
    return { outcome: 'error', score: null };
  }
  let score = null;
  for (let i = 0; i < 20 && score === null; i++) { await sleep(1000); score = await tab.evaluate(SCORE).catch(() => null); }
  await tab.close().catch(() => {});
  return { outcome: score === null ? 'no score' : 'score', score };
}

// hCaptcha only decides when someone clicks its checkbox, so a load shows whether the widget renders, nothing more.
async function hcaptcha(context) {
  const tab = await context.newPage();
  try {
    await tab.goto(HCAPTCHA, { waitUntil: 'domcontentloaded', timeout: 45000 });
  } catch {
    await tab.close();
    return { outcome: 'error' };
  }
  let shown = false;
  for (let i = 0; i < 12 && !shown; i++) { await sleep(1000); shown = await tab.evaluate(HC_WIDGET).catch(() => false); }
  await tab.close().catch(() => {});
  return { outcome: shown ? 'widget shown' : 'no widget' };
}

async function row(setupKey, network) {
  const s = SETUPS[setupKey];
  const out = { setup: s.setup, network: NETWORKS[network].label };
  let browser;
  try {
    browser = await launch(s, network);
  } catch (err) {
    return { ...out, recaptcha: { outcome: 'error', score: null }, hcaptcha: { outcome: 'error' } };
  }
  // A session the server ends (e.g. its memory limit) leaves the remaining cells as errors.
  for (const [key, fn] of [['recaptcha', recaptcha], ['hcaptcha', hcaptcha]]) {
    try {
      const context = await browser.newContext(); // a fresh context: every load is a first visit
      out[key] = await fn(context);
      await context.close();
    } catch {
      out[key] = key === 'recaptcha' ? { outcome: 'error', score: null } : { outcome: 'error' };
    }
  }
  await browser.close().catch(() => {});
  return out;
}

const rows = [];
for (const network of ['own', 'tier1']) {
  for (const setupKey of Object.keys(SETUPS)) rows.push(await row(setupKey, network));
}
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), rows }, null, 2));
