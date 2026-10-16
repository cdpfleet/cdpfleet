// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (no proxy of your own needed: the cdpfleet residential proxy, billed per GB by tier)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGE = 'https://commons.wikimedia.org/wiki/Category:Cats';
const PRICE = { 1: 5, 4: 0.5 }; // USD per GB (10^9 bytes)
const CDN = ['thumb.wikimedia.org', 'upload.wikimedia.org']; // the images; HTML, CSS and JS come from commons.wikimedia.org

// One session, two prices: the page through Tier 1, the image hosts through Tier 4.
const LAUNCH = {
  headless: 'new',
  record: false,
  proxy: 'cdpfleet-resi-country-us',
  proxy_rules: [{ hosts: CDN, proxy: 'cdpfleet-resi-tier-4-country-us' }],
};

// Today's residential traffic per tier, as billed.
async function usedToday() {
  const b = await (await fetch('https://cdpfleet.com/v1/me/proxy-balance', { headers: { 'x-api-key': KEY } })).json();
  return Object.fromEntries(b.used_today.map((t) => [t.tier, t.bytes]));
}

// Load the gallery, scroll to the end, and add up every request's bytes by host group.
async function load() {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify(LAUNCH),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    const done = [];
    page.on('requestfinished', (req) => done.push(req));
    // Residential peers drop the odd connection: retry the navigation once.
    for (let attempt = 1; ; attempt++) {
      try { await page.goto(PAGE, { waitUntil: 'load', timeout: 60000 }); break; } catch (err) { if (attempt === 2) throw err; }
    }
    for (let i = 0; i < 20; i++) { // lazy-loaded thumbnails load as they scroll into view
      await page.mouse.wheel(0, 1500);
      await page.waitForTimeout(250);
    }
    await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});
    const bytes = { page: 0, images: 0 };
    let images = 0;
    for (const req of done) {
      const s = await req.sizes().catch(() => null);
      if (!s) continue;
      const total = s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
      if (CDN.includes(new URL(req.url()).hostname)) { bytes.images += total; images++; } else bytes.page += total;
    }
    return { requests: done.length, image_requests: images, page_kb: Math.round(bytes.page / 1000), image_kb: Math.round(bytes.images / 1000) };
  } finally {
    await browser.close();
  }
}

const before = await usedToday();
const run = await load();

// Metering trails the traffic by about a minute: wait until Tier 4 shows the images.
let after = before;
for (let i = 0; i < 18 && ((after[4] || 0) - (before[4] || 0)) / 1000 < run.image_kb * 0.8; i++) {
  await new Promise((r) => setTimeout(r, 10000));
  after = await usedToday();
}

// What 1,000 such loads cost: everything on Tier 1 versus the split.
const per1000 = (pageTier, imageTier) => Math.round((run.page_kb * PRICE[pageTier] + run.image_kb * PRICE[imageTier]) / 1e6 * 1000 * 100) / 100;
console.log(JSON.stringify({
  page: PAGE,
  run,
  image_share: Math.round((run.image_kb / (run.image_kb + run.page_kb)) * 100) / 100,
  cost_per_1000_loads_usd: { all_tier_1: per1000(1, 1), page_tier_1_images_tier_4: per1000(1, 4) },
  billed_kb: {
    tier_1: Math.round(((after[1] || 0) - (before[1] || 0)) / 1000),
    tier_4: Math.round(((after[4] || 0) - (before[4] || 0)) / 1000),
  },
}, null, 2));
