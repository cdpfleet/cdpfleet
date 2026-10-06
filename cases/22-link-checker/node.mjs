// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const START = 'https://books.toscrape.com/'; // 70-odd same-site links on the front page
const WAVE = 8; // links checked at once

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

const summary = (method, results, seconds, extra = {}) => ({
  method,
  links: results.length,
  ok: results.filter((r) => r.status >= 200 && r.status < 300).length,
  redirected: results.filter((r) => r.redirected).length,
  broken: results.filter((r) => r.status === 0 || r.status >= 400).length,
  broken_urls: results.filter((r) => r.status === 0 || r.status >= 400).map((r) => r.url).slice(0, 5),
  seconds,
  seconds_per_link: Math.round((seconds / results.length) * 100) / 100,
  ...extra,
});

try {
  const page = await browser.newPage();
  await page.goto(START, { timeout: 60000 });
  // Every unique same-site link on the page, as absolute URLs.
  const links = await page.$$eval('a[href]', (as, origin) => [...new Set(as.map((a) => a.href))].filter((h) => h.startsWith(origin) && !h.includes('#')), new URL(START).origin);

  // Way 1: fetch() inside the page, WAVE links at a time — the browser's proxy, cookies
  // and TLS, no navigation, no rendering, no assets.
  const t1 = Date.now();
  const fetched = [];
  for (let i = 0; i < links.length; i += WAVE) {
    const wave = links.slice(i, i + WAVE);
    fetched.push(...await page.evaluate((urls) => Promise.all(urls.map(async (url) => {
      try { const r = await fetch(url, { cache: 'no-store' }); return { url, status: r.status, redirected: r.redirected }; } catch { return { url, status: 0, redirected: false }; }
    })), wave));
  }
  const inPage = summary(`fetch() in the page, ${WAVE} at a time`, fetched, (Date.now() - t1) / 1000);

  // Way 2: navigate to each link, like a user — full page loads with all their assets.
  // Only a sample: this is the slow way, and it is the same work for every link.
  const sample = links.slice(0, 10);
  let requests = 0;
  page.on('request', () => { requests++; });
  const t2 = Date.now();
  const navigated = [];
  for (const url of sample) {
    try { const r = await page.goto(url, { timeout: 60000 }); navigated.push({ url, status: r ? r.status() : 0, redirected: r ? r.url() !== url : false }); } catch { navigated.push({ url, status: 0, redirected: false }); }
  }
  const byNav = summary('page.goto each link (10-link sample)', navigated, (Date.now() - t2) / 1000, { requests_made: requests });

  console.log(JSON.stringify([{ ...inPage, requests_made: links.length }, byNav], null, 2));
} finally {
  await browser.close();
}
