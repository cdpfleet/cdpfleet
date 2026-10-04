// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const START = 'https://quotes.toscrape.com/scroll'; // loads 10 quotes per screen, 100 in all

// Way 1: behave like a user — scroll to the bottom until nothing more appears, then read the DOM.
async function byScrolling(page) {
  const t = Date.now();
  let requests = 0;
  page.on('request', () => { requests++; });
  await page.goto(START, { timeout: 60000 });
  await page.locator('.quote').first().waitFor({ timeout: 60000 });
  const loaded = Date.now();
  let count = 0;
  let scrolls = 0;
  let stale = 0;
  while (stale < 3) {
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    scrolls++;
    await page.waitForTimeout(600);
    const now = await page.locator('.quote').count();
    stale = now > count ? 0 : stale + 1;
    count = now;
  }
  const quotes = await page.$$eval('.quote', (els) => els.map((e) => ({ text: e.querySelector('.text').textContent, author: e.querySelector('.author').textContent })));
  return { method: 'scroll the page', quotes: quotes.length, authors: new Set(quotes.map((q) => q.author)).size, scrolls, requests, load_seconds: (loaded - t) / 1000, collect_seconds: (Date.now() - loaded) / 1000 };
}

// Way 2: catch the JSON request the page makes for its first screen, then call that endpoint
// yourself from inside the browser (same proxy, cookies and TLS fingerprint as the page),
// several pages at a time. No scrolling, no guessing when loading has finished.
async function byApi(page) {
  const t = Date.now();
  let requests = 0;
  page.on('request', () => { requests++; });
  const first = page.waitForResponse((r) => r.url().includes('/api/') && r.request().resourceType() === 'xhr', { timeout: 60000 });
  await page.goto(START, { timeout: 60000 });
  const response = await first;
  const loaded = Date.now();
  const endpoint = new URL(response.url());
  const quotes = [...(await response.json()).quotes]; // page 1 came for free
  const WAVE = 4; // one round trip through the proxy per wave, not per page
  let next = 2;
  for (let more = true; more; next += WAVE) {
    const urls = Array.from({ length: WAVE }, (_, i) => { endpoint.searchParams.set('page', String(next + i)); return endpoint.href; });
    const pages = await page.evaluate((us) => Promise.all(us.map((u) => fetch(u).then((r) => r.json()))), urls);
    for (const p of pages) quotes.push(...p.quotes);
    more = pages.every((p) => p.has_next);
  }
  return { method: 'call its JSON API', quotes: quotes.length, authors: new Set(quotes.map((q) => q.author.name)).size, api_pages: next - 1, endpoint: `${endpoint.pathname}?page=N`, requests, load_seconds: (loaded - t) / 1000, collect_seconds: (Date.now() - loaded) / 1000 };
}

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const out = [];
  for (const way of [byScrolling, byApi]) {
    const page = await browser.newPage(); // a fresh tab per method, so request counts don't mix
    out.push(await way(page));
    await page.close();
  }
  console.log(JSON.stringify(out, null, 2));
} finally {
  await browser.close();
}
