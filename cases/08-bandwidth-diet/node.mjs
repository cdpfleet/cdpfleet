// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGE = 'https://www.bbc.com/news';
const FIRST_PARTY = ['bbc.com', 'bbc.co.uk', 'bbci.co.uk']; // the site's own domains and CDNs
const PRICE_PER_GB = 3; // a typical residential proxy price, USD

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

const HEAVY = new Set(['image', 'media', 'font']);

// Load the page in a fresh context (empty cache) and count every byte on the wire.
async function measure(label, block) {
  const context = await browser.newContext();
  if (block) {
    await context.route('**/*', (route) => {
      const r = route.request();
      if (HEAVY.has(r.resourceType()) || (block === 'first-party' && !FIRST_PARTY.some((d) => new URL(r.url()).hostname.endsWith(d)))) {
        return route.abort();
      }
      return route.continue();
    });
  }
  const page = await context.newPage();
  let bytes = 0;
  let requests = 0;
  let blocked = 0;
  const count = async (req) => {
    const s = await req.sizes();
    bytes += s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
    requests++;
  };
  page.on('requestfinished', count);
  page.on('requestfailed', () => { blocked++; });
  const t = Date.now();
  // DOM ready, then a fixed 5 s for the rest to arrive: the same window for every variant
  // ('load' can wait forever on a blocked video).
  await page.goto(PAGE, { waitUntil: 'domcontentloaded', timeout: 90000 });
  const readyMs = Date.now() - t;
  await page.waitForTimeout(5000);
  const title = await page.title();
  page.off('requestfinished', count); // stop counting before closing
  await context.close();
  return { variant: label, title, requests, blocked, kilobytes: Math.round(bytes / 1024), dom_ready_ms: readyMs };
}

try {
  const rows = [
    await measure('everything', null),
    await measure('no images, media or fonts', 'heavy'),
    await measure('…and first-party only', 'first-party'),
  ];
  const full = rows[0].kilobytes;
  for (const r of rows) {
    r.saved = `${Math.round((1 - r.kilobytes / full) * 100)}%`;
    r.proxy_cost_per_100k_pages = `$${((r.kilobytes * 100000) / 1024 / 1024 * PRICE_PER_GB).toFixed(0)}`;
  }
  console.log(JSON.stringify(rows, null, 2));
} finally {
  await browser.close();
}
