// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import { randomBytes } from 'node:crypto';
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// Every setup gets its own sticky residential exit (letter = setup): DataDome scores the IP as well as the browser,
// so one setup's visits must not change what the next one sees.
const NETWORKS = {
  own: { label: 'own proxy (rotating)', proxy: () => process.env.PROXY_URL },
  tier1: { label: 'cdpfleet-resi Tier 1, sticky US', proxy: (id) => `cdpfleet-resi-country-us-session-d${RUN}${id}-lifetime-30` },
  tier4: { label: 'cdpfleet-resi Tier 4, sticky US', proxy: (id) => `cdpfleet-resi-tier-4-country-us-session-d${RUN}${id}-lifetime-30` },
};
const SETUPS = {
  chromium: { id: 'a', setup: 'Chromium, headless', engine: 'chromium', type: chromium, headless: true },
  chrome: { id: 'b', setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, headless: false },
  edge: { id: 'c', setup: 'Microsoft Edge, headful', engine: 'edge', type: chromium, headless: false },
  camoufox: { id: 'd', setup: 'Camoufox, defaults', engine: 'camoufox', type: firefox, headless: true },
};
// DataDome's own public site, which DataDome protects: its home page and its pricing page.
const PAGES = {
  home: 'https://datadome.co/',
  pricing: 'https://datadome.co/pricing/',
};

// What the page shows right now, in the lab's words. DataDome answers an intercepted request with a 403 page that
// embeds an iframe from captcha-delivery.com: a device check that passes by itself, a CAPTCHA, or a block (t=bv).
// The same expression runs in every language.
const STATE = `(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\\/captcha\\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()`;

// 503 means the fleet is momentarily busy: wait Retry-After and try again. A profile's second session can land on a
// node with an older build of the browser (409 profile_version_downgrade): ask again for another node.
async function launch(s, network, extra = {}) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${s.engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: NETWORKS[network].proxy(extra.exit || s.id), headless: s.headless, record: false, ...(extra.body || {}) }),
    });
    if (res.ok) return s.type.connect((await res.json()).wsUrl, { headers: { 'x-api-key': KEY } });
    const text = await res.text();
    const retry = [429, 503].includes(res.status) || (res.status === 409 && text.includes('profile_version_downgrade'));
    if (!retry || attempt === 8) throw new Error(`launch ${res.status} ${text}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Load a page (a network failure is retried once), then watch it once a second for 12 s without touching it.
// content = the page straight away; cleared = DataDome intercepted (403 or its device check) and let the browser
// through by itself; challenge = a CAPTCHA (never solved); blocked = DataDome's block page.
async function visit(tab, url) {
  const t0 = Date.now();
  let status = null;
  try {
    try {
      status = (await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? null;
    } catch {
      status = (await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 }))?.status() ?? null;
    }
  } catch {
    return { outcome: 'error', status: null, seconds: null };
  }
  let checked = status === 403;
  const until = Date.now() + 12000; // the watch starts once the page has loaded, however slow the proxy was
  while (Date.now() < until) {
    const s = await tab.evaluate(STATE).catch(() => null); // null while the device check reloads the page
    if (s === 'blocked' || s === 'challenge') return { outcome: s, status, seconds: null };
    if (s === 'checking') checked = true;
    if (s === 'page') return checked ? { outcome: 'cleared', status, seconds: Math.round((Date.now() - t0) / 1000) } : { outcome: 'content', status, seconds: null };
    await sleep(1000);
  }
  return { outcome: checked ? 'challenge' : 'error', status, seconds: null };
}

// One session per setup and network; a fresh context per page so every load is a first visit.
async function row(setupKey, network, pages) {
  const s = SETUPS[setupKey];
  const browser = await launch(s, network);
  const out = { setup: s.setup, network: NETWORKS[network].label };
  try {
    for (const key of pages) {
      const context = await browser.newContext();
      out[key] = await visit(await context.newPage(), PAGES[key]);
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

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart. The profile keeps
// the datadome cookie the first visit was given.
async function returning(setupKey) {
  const name = `case65-${RUN}-${SETUPS[setupKey].id}`;
  const exit = `${SETUPS[setupKey].id}p`;
  const visits = [];
  for (let i = 0; i < 2; i++) {
    const browser = await launch(SETUPS[setupKey], 'tier1', { exit, body: { profile: name } });
    try {
      const context = browser.contexts()[0]; // the profile's own context; a newContext() is not saved
      visits.push(await visit(await context.newPage(), PAGES.home));
      // DataDome's script sets its cookie a moment after the page: give it up to 5 s.
      const hasCookie = async () => (await context.cookies()).some((c) => c.name === 'datadome');
      for (let j = 0; j < 5 && !(await hasCookie()); j++) await sleep(1000);
      visits[i].cookie = await hasCookie();
    } finally {
      await browser.close();
    }
    // The node saves the profile after the browser exits: wait until it is free again.
    for (let j = 0; j < 60 && (await profileInfo(name))?.in_use !== false; j++) await sleep(2000);
  }
  await api('DELETE', `/v1/me/profiles/${name}`);
  return { setup: SETUPS[setupKey].setup, network: NETWORKS.tier1.label, first: visits[0], second: visits[1] };
}

// Camoufox with humanize on and off: two page views in one context, with three mouse moves between them, so DataDome's
// script on the first page can see how the cursor moves before the second page is requested.
async function humanize(on) {
  const s = SETUPS.camoufox;
  const browser = await launch(s, 'tier1', { exit: on ? 'h1' : 'h0', body: { humanize: on } });
  try {
    const tab = await (await browser.newContext()).newPage();
    const first = await visit(tab, PAGES.home);
    const t0 = Date.now();
    for (const [x, y] of [[200, 300], [700, 250], [450, 600]]) {
      await Promise.race([tab.mouse.move(x, y), sleep(15000)]);
    }
    const move_seconds = Math.round((Date.now() - t0) / 100) / 10;
    const second = await visit(tab, PAGES.pricing);
    return { setup: `Camoufox, humanize ${on ? 'on' : 'off'}`, network: NETWORKS.tier1.label, home: first, pricing: second, move_seconds };
  } finally {
    await browser.close();
  }
}

const matrix = [];
for (const network of ['own', 'tier1']) {
  for (const setupKey of ['chromium', 'chrome', 'edge', 'camoufox']) matrix.push(await row(setupKey, network, Object.keys(PAGES)));
}
const tier4 = [];
for (const setupKey of ['chrome', 'camoufox']) tier4.push(await row(setupKey, 'tier4', ['home']));
const profiles = [await returning('chrome'), await returning('camoufox')];
const human = [await humanize(true), await humanize(false)];

console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), matrix, tier4, profiles, humanize: human }, null, 2));
