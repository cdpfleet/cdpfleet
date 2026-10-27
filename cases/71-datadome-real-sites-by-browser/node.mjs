// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, LAB_SITES_DATADOME (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import { randomBytes } from 'node:crypto';
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
const SITES = process.env.LAB_SITES_DATADOME.split(',').map((url, i) => ({ site: String.fromCharCode(65 + i), url }));
// Every browser the fleet offers that a Playwright 1.60 client can drive (Rebrowser needs 1.52, so it's left out).
const SETUPS = [
  ['chrome_headful', 'Chrome, headful', 'chrome', chromium, false],
  ['chrome_headless', 'Chrome, headless', 'chrome', chromium, 'new'],
  ['edge', 'Edge, headful', 'edge', chromium, false],
  ['brave', 'Brave, headful', 'brave', chromium, false],
  ['opera', 'Opera, headful', 'opera', chromium, false],
  ['yandex', 'Yandex, headful', 'yandex', chromium, false],
  ['whale', 'Whale, headful', 'whale', chromium, false],
  ['chromium_headless', 'Chromium, headless', 'chromium', chromium, true],
  ['patchright', 'Patchright, headful', 'patchright', chromium, false],
  ['firefox', 'Firefox, headful', 'firefox', firefox, false],
  ['webkit', 'WebKit, headful', 'webkit', webkit, false],
  ['camoufox', 'Camoufox, headful', 'camoufox', firefox, false],
];

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words.
const STATE = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\\s+/g, ' ');
  const len = document.body ? document.body.innerText.length : 0;
  if ([...document.querySelectorAll('iframe')].some((f) => (f.src || '').includes('captcha-delivery.com'))) return 'challenge';
  if (/you have been blocked|access is temporarily restricted|attention required|error 1020|access denied/i.test(t) && len < 4000) return 'blocked';
  if (/just a moment|performing security verification|verify you are human|checking your browser|checking if the site connection is secure/i.test(t) && len < 4000) return 'challenge';
  return 'content';
})()`;

async function launch(engine, headless, proxy, profile) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy, headless, record: false, ...(profile ? { profile } : {}) }),
    });
    if (res.ok) return (await res.json()).wsUrl;
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// One session, one load: the status of the page's own response, then its state 8 s later.
async function visit(setup, url, proxy, profile) {
  const [, , engine, type, headless] = setup;
  const browser = await type.connect(await launch(engine, headless, proxy, profile), { headers: { 'x-api-key': KEY } });
  try {
    const context = profile ? browser.contexts()[0] : await browser.newContext();
    const tab = await context.newPage();
    let status = 0;
    try {
      status = (await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? 0;
    } catch {
      return { outcome: 'error', status: 0 };
    }
    await tab.waitForTimeout(8000);
    let outcome = await tab.evaluate(STATE).catch(() => 'error');
    if (outcome === 'content' && status >= 400) outcome = 'blocked';
    return { outcome, status };
  } finally {
    await browser.close();
  }
}

async function api(method, path) {
  const res = await fetch(`https://cdpfleet.com${path}`, { method, headers: { 'x-api-key': KEY } });
  return res.json().catch(() => ({}));
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// One sticky residential exit per site; its setups run one after another (one session at a time per
// site), and the sites run side by side. A network error gets one more try.
async function site({ site, url }) {
  const proxy = `cdpfleet-resi-country-us-session-c71${RUN}${site}-lifetime-30`;
  const results = {};
  for (const setup of SETUPS) {
    let r = await visit(setup, url, proxy).catch(() => ({ outcome: 'error', status: 0 }));
    if (r.outcome === 'error') r = await visit(setup, url, proxy).catch(() => ({ outcome: 'error', status: 0 }));
    results[setup[0]] = r;
  }
  // A returning visitor: Camoufox with a profile, a first visit and then a second session.
  const name = `case71-${RUN}-${site}`;
  const camoufox = SETUPS.find((s) => s[0] === 'camoufox');
  await visit(camoufox, url, proxy, name).catch(() => null);
  for (let j = 0; j < 60 && (await api('GET', '/v1/me/profiles')).profiles?.find((p) => p.name === name)?.in_use !== false; j++) await sleep(2000);
  results.camoufox_returning = await visit(camoufox, url, proxy, name).catch(() => ({ outcome: 'error', status: 0 }));
  await api('DELETE', `/v1/me/profiles/${name}`);
  return { site, results };
}

const sites = await Promise.all(SITES.map(site));
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), setups: Object.fromEntries([...SETUPS.map((s) => [s[0], s[1]]), ['camoufox_returning', 'Camoufox, returning profile (2nd visit)']]), sites }, null, 2));
