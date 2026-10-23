// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import { randomBytes } from 'node:crypto';
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
const NETWORKS = {
  own: { label: 'own proxy (rotating)', proxy: process.env.PROXY_URL },
  tier1: { label: 'cdpfleet-resi Tier 1, sticky US', proxy: `cdpfleet-resi-country-us-session-c67${RUN}-lifetime-30` },
  tier4: { label: 'cdpfleet-resi Tier 4, sticky US', proxy: `cdpfleet-resi-tier-4-country-us-session-c67${RUN}-lifetime-30` },
};
const SETUPS = {
  chromium: { setup: 'Chromium, headless', engine: 'chromium', type: chromium, headless: true },
  chrome: { setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, headless: false },
  edge: { setup: 'Microsoft Edge, headful', engine: 'edge', type: chromium, headless: false },
  camoufox: { setup: 'Camoufox, defaults', engine: 'camoufox', type: firefox, headless: true },
};
// Each vendor's own home page, which that vendor protects with its own product.
const PAGES = {
  akamai: { url: 'https://www.akamai.com/', sensor: (u) => new URL(u).host === 'www.akamai.com' },
  human: { url: 'https://www.humansecurity.com/', sensor: (u) => /(^|\.)px-(cloud|cdn)\.net$/.test(new URL(u).host) },
};

// What the page shows right now, in the lab's words. The same expression runs in every language.
const STATE = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
  const blocked = /access denied|you don't have permission to access|request blocked|reference #\\d/i.test(t);
  const challenge = !!document.querySelector('#px-captcha, #sec-if-cpt-container, #sec-cpt-if') || /press (&|and) hold|challenge validation/i.test(t);
  return blocked ? 'blocked' : challenge ? 'challenge' : 'page';
})()`;

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(s, network, profile) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${s.engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: NETWORKS[network].proxy, headless: s.headless, record: false, ...(profile ? { profile } : {}) }),
    });
    if (res.ok) return s.type.connect((await res.json()).wsUrl, { headers: { 'x-api-key': KEY } });
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Akamai's _abck carries a status between its first two "~": -1 until a sensor post is accepted. Only that
// field is kept, never the cookie itself. HUMAN issues _px3 once its collector accepts the browser.
async function signals(context) {
  const jar = await context.cookies();
  const abck = jar.find((c) => c.name === '_abck')?.value;
  return { abck: abck ? abck.split('~')[1] ?? '?' : null, px3: jar.some((c) => c.name === '_px3') };
}

// One load, then watch the page once a second for 10 s without touching it, so the vendor's script
// can post its sensor data; a network failure is retried once.
async function visit(context, key) {
  const page = PAGES[key];
  const tab = await context.newPage();
  let posts = 0;
  tab.on('request', (r) => { try { if (r.method() === 'POST' && page.sensor(r.url())) posts++; } catch {} });
  let status = null;
  try {
    try {
      status = (await tab.goto(page.url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? null;
    } catch {
      status = (await tab.goto(page.url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? null;
    }
  } catch {
    await tab.close();
    return { outcome: 'error' };
  }
  const atLoad = await signals(context);
  let seen = new Set();
  for (let i = 0; i < 10; i++) {
    const s = await tab.evaluate(STATE).catch(() => null); // null while the page navigates
    if (s) seen.add(s);
    if (s === 'blocked') break;
    await sleep(1000);
  }
  const last = await tab.evaluate(STATE).catch(() => null);
  const outcome = seen.has('blocked') || last === 'blocked' ? 'blocked'
    : last === 'challenge' ? 'challenge' : seen.has('challenge') ? 'cleared' : 'content';
  const after = await signals(context);
  await tab.close();
  const cell = { outcome, status, sensor_posts: posts };
  if (key === 'akamai') Object.assign(cell, { abck_at_load: atLoad.abck, abck_after: after.abck });
  else cell.px3 = after.px3;
  return cell;
}

// One session per setup and network; a fresh context per page so every load is a first visit.
async function row(setupKey, network, pages) {
  const s = SETUPS[setupKey];
  const browser = await launch(s, network);
  const out = { setup: s.setup, network: NETWORKS[network].label };
  try {
    for (const key of pages) {
      const context = await browser.newContext();
      out[key] = await visit(context, key);
      await context.close();
    }
  } finally {
    await browser.close();
  }
  return out;
}

async function api(method, path) {
  const res = await fetch(`https://cdpfleet.com${path}`, { method, headers: { 'x-api-key': KEY } });
  return res.json().catch(() => ({}));
}
const profileInfo = async (name) => (await api('GET', '/v1/me/profiles')).profiles?.find((p) => p.name === name);

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
async function returning(setupKey, network, key) {
  const name = `case67-${RUN}`;
  const visits = [];
  for (let i = 0; i < 2; i++) {
    const browser = await launch(SETUPS[setupKey], network, name);
    try {
      visits.push(await visit(browser.contexts()[0], key)); // the profile's own context; a newContext() is not saved
    } finally {
      await browser.close();
    }
    // The node saves the profile after the browser exits: wait until it is free again.
    for (let j = 0; j < 60 && (await profileInfo(name))?.in_use !== false; j++) await sleep(2000);
  }
  await api('DELETE', `/v1/me/profiles/${name}`);
  return { setup: SETUPS[setupKey].setup, network: NETWORKS[network].label, page: key, first: visits[0], second: visits[1] };
}

const matrix = [];
for (const network of ['own', 'tier1']) {
  for (const setupKey of ['chromium', 'chrome', 'edge', 'camoufox']) matrix.push(await row(setupKey, network, Object.keys(PAGES)));
}
const tier4 = [];
for (const setupKey of ['chrome', 'camoufox']) tier4.push(await row(setupKey, 'tier4', Object.keys(PAGES)));
const profile = await returning('chrome', 'tier1', 'akamai');

console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), matrix, tier4, profile }, null, 2));
