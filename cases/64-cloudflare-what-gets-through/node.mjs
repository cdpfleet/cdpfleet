// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
import { randomBytes } from 'node:crypto';
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
const NETWORKS = {
  own: { label: 'own proxy (rotating)', proxy: process.env.PROXY_URL },
  tier1: { label: 'cdpfleet-resi Tier 1, sticky US', proxy: `cdpfleet-resi-country-us-session-c64${RUN}-lifetime-30` },
  tier4: { label: 'cdpfleet-resi Tier 4, sticky US', proxy: `cdpfleet-resi-tier-4-country-us-session-c64${RUN}-lifetime-30` },
};
const SETUPS = {
  chromium: { setup: 'Chromium, headless', engine: 'chromium', type: chromium, headless: true },
  chrome: { setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, headless: false },
  edge: { setup: 'Microsoft Edge, headful', engine: 'edge', type: chromium, headless: false },
  camoufox: { setup: 'Camoufox, defaults', engine: 'camoufox', type: firefox, headless: true },
};
// Public test pages only: two Cloudflare challenge pages and a Turnstile test page in two widget modes. `cf` pages may
// answer with Cloudflare's interstitial; `widget` pages embed Turnstile and get a token if it passes.
const PAGES = {
  nowsecure: { url: 'https://nowsecure.nl/', kind: 'cf' },
  scrapingcourse: { url: 'https://www.scrapingcourse.com/cloudflare-challenge', kind: 'cf' },
  ts_noninteractive: { url: 'https://peet.ws/turnstile-test/non-interactive.html', kind: 'widget' },
  ts_managed: { url: 'https://peet.ws/turnstile-test/managed.html', kind: 'widget' },
};

// What the page shows right now, in the lab's words. The same expression runs in every language.
const STATE = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
  const el = document.querySelector('[name="cf-turnstile-response"]');
  const state = /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
  return { state, token: el && el.value ? el.value.length : 0 };
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
const hasClearance = async (context) => (await context.cookies()).some((c) => c.name === 'cf_clearance');

// One load, then watch the page once a second without touching it: an interstitial gets 20 s to
// clear by itself, a Turnstile widget 12 s to issue a token. A network failure is retried once.
async function visit(context, key) {
  const page = PAGES[key];
  const tab = await context.newPage();
  const t0 = Date.now();
  const cell = { outcome: 'challenge', seconds: null };
  try {
    try {
      await tab.goto(page.url, { waitUntil: 'domcontentloaded', timeout: 45000 });
    } catch {
      await tab.goto(page.url, { waitUntil: 'domcontentloaded', timeout: 45000 });
    }
  } catch {
    await tab.close();
    return { outcome: 'error', seconds: null };
  }
  let sawChallenge = false;
  const deadline = t0 + (page.kind === 'widget' ? 12000 : 20000);
  while (Date.now() < deadline) {
    const s = await tab.evaluate(STATE).catch(() => null); // null while the interstitial reloads the page
    if (s?.state === 'blocked') { cell.outcome = 'blocked'; break; }
    if (s?.state === 'challenge') sawChallenge = true;
    if (s && page.kind === 'widget' && s.token > 0) { cell.outcome = 'cleared'; cell.seconds = Math.round((Date.now() - t0) / 1000); break; }
    if (s?.state === 'page' && page.kind === 'cf') {
      cell.outcome = sawChallenge ? 'cleared' : 'content';
      if (sawChallenge) cell.seconds = Math.round((Date.now() - t0) / 1000);
      break;
    }
    await sleep(1000);
  }
  if (page.kind === 'cf') {
    // The clearance cookie can arrive a moment after the content: give it up to 6 s.
    for (let i = 0; i < 6 && cell.outcome !== 'blocked' && !(await hasClearance(context)); i++) await sleep(1000);
    cell.cf_clearance = await hasClearance(context);
  } else {
    cell.token = cell.outcome === 'cleared';
  }
  await tab.close();
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
  const name = `case64-${RUN}`;
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
for (const setupKey of ['chrome', 'camoufox']) tier4.push(await row(setupKey, 'tier4', ['nowsecure', 'scrapingcourse', 'ts_managed']));
const profile = await returning('camoufox', 'tier1', 'scrapingcourse');

console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), matrix, tier4, profile }, null, 2));
