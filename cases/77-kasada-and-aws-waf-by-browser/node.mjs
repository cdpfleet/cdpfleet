// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, LAB_SITES_KASADA_AWSWAF (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
import { randomBytes } from 'node:crypto';
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
const SITES = process.env.LAB_SITES_KASADA_AWSWAF.split(',').map((url, i) => ({ site: String.fromCharCode(65 + i), url }));
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

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words. AWS WAF's
// CAPTCHA ("Let's confirm you are human") is interactive: a challenge. A near-empty page 8 s later
// (Kasada's 429 answer is a blank page with a script) or an "access denied" page is blocked.
const STATE = `(() => {
  const text = document.body ? document.body.innerText : '';
  const t = (document.title + ' ' + text.slice(0, 600)).replace(/\\s+/g, ' ');
  if (document.querySelector('#captcha-container, awswaf-captcha') || (/confirm you are human|verify you are (a )?human|are you a robot|solve the puzzle/i.test(t) && text.length < 4000)) return 'challenge';
  if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request (unsuccessful|blocked)|too many requests/i.test(t) && text.length < 4000) return 'blocked';
  if (text.trim().length < 200) return 'blank';
  return 'content';
})()`;

const KASADA_URL = /\/ips\.js(\?|$)|\/(tl|fp)(\?|$)/;
const AWS_URL = /awswaf\.com|\/challenge\.js(\?|$)/;

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

const failed = () => ({ outcome: 'error', status: 0, final_status: 0, kasada: false, aws_waf: false, cookies: [] });

// One session, one load. Records the status of the page's first response and of the last main-frame
// document 8 s later (an interstitial that clears itself reloads the page), which vendor's scripts,
// headers or cookies showed up (cookie and header NAMES only; values are never read), and the state.
async function visit(setup, url, proxy, profile) {
  const [, , engine, type, headless] = setup;
  const browser = await type.connect(await launch(engine, headless, proxy, profile), { headers: { 'x-api-key': KEY } });
  try {
    const context = profile ? browser.contexts()[0] : await browser.newContext();
    const tab = await context.newPage();
    const seen = { kasada: false, aws_waf: false, waf_action: null, docs: [] };
    const t0 = Date.now();
    tab.on('request', (r) => {
      if (KASADA_URL.test(r.url()) || Object.keys(r.headers()).some((h) => h.startsWith('x-kpsdk'))) seen.kasada = true;
      if (AWS_URL.test(r.url())) seen.aws_waf = true;
    });
    tab.on('response', (r) => {
      const h = r.headers();
      if (Object.keys(h).some((n) => n.startsWith('x-kpsdk'))) seen.kasada = true;
      if (h['x-amzn-waf-action']) { seen.aws_waf = true; if (/^(challenge|captcha)$/.test(h['x-amzn-waf-action'])) seen.waf_action ??= h['x-amzn-waf-action']; }
      if (r.request().isNavigationRequest() && r.frame() === tab.mainFrame()) seen.docs.push([r.status(), Date.now() - t0]);
    });
    let status = 0;
    try {
      status = (await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? 0;
    } catch (err) {
      // A reset HTTP/2 connection is a refusal, not a network failure.
      return { ...failed(), outcome: /ERR_HTTP2_PROTOCOL_ERROR/.test(err.message) ? 'blocked' : 'error' };
    }
    await tab.waitForTimeout(8000);
    let outcome = await tab.evaluate(STATE).catch(() => 'error');
    const [finalStatus, finalAt] = seen.docs.at(-1) || [status, 0];
    let clearedS = null;
    // A 202/429 check that passed by itself reloads the page with a success status: cleared, even if
    // the new page hasn't rendered yet at the 8 s mark. Without that reload it never cleared.
    if (outcome === 'content' || outcome === 'blank') {
      if (finalStatus >= 400) outcome = 'blocked';
      else if (status >= 400 || status === 202) {
        if (seen.docs.length > 1 && finalStatus !== 202) { outcome = 'cleared'; clearedS = Math.round(finalAt / 100) / 10; } else outcome = 'blocked';
      } else if (outcome === 'blank') outcome = 'blocked';
    }
    const cookies = (await context.cookies()).map((c) => c.name).filter((n) => /^(KP_|aws-waf)/i.test(n)).sort();
    if (cookies.some((n) => /^KP_/i.test(n))) seen.kasada = true;
    if (cookies.some((n) => /^aws-waf/i.test(n))) seen.aws_waf = true;
    return { outcome, status, final_status: finalStatus, ...(clearedS != null ? { cleared_s: clearedS } : {}), kasada: seen.kasada, aws_waf: seen.aws_waf, ...(seen.waf_action ? { waf_action: seen.waf_action } : {}), cookies: [...new Set(cookies)] };
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
  const proxy = `cdpfleet-resi-country-us-session-c77${RUN}${site}-lifetime-30`;
  const results = {};
  for (const setup of SETUPS) {
    let r = await visit(setup, url, proxy).catch(failed);
    if (r.outcome === 'error') r = await visit(setup, url, proxy).catch(failed);
    results[setup[0]] = r;
  }
  // A returning visitor: Camoufox with a profile, a first visit and then a second session.
  const name = `case77-${RUN}-${site}`;
  const camoufox = SETUPS.find((s) => s[0] === 'camoufox');
  await visit(camoufox, url, proxy, name).catch(() => null);
  for (let j = 0; j < 60 && (await api('GET', '/v1/me/profiles')).profiles?.find((p) => p.name === name)?.in_use !== false; j++) await sleep(2000);
  results.camoufox_returning = await visit(camoufox, url, proxy, name).catch(failed);
  await api('DELETE', `/v1/me/profiles/${name}`);
  return { site, results };
}

const sites = await Promise.all(SITES.map(site));
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), setups: Object.fromEntries([...SETUPS.map((s) => [s[0], s[1]]), ['camoufox_returning', 'Camoufox, returning profile (2nd visit)']]), sites }, null, 2));
