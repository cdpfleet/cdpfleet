// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
// A static catalogue, a big article and a page that renders its data with a delayed script.
const PAGES = [
  { url: 'https://books.toscrape.com/', data: 'article.product_pod' },
  { url: 'https://en.wikipedia.org/wiki/Web_scraping', data: '#mw-content-text p' },
  { url: 'https://quotes.toscrape.com/js-delayed/', data: '.quote' },
];
const STRATEGIES = ['commit', 'domcontentloaded', 'load', 'networkidle', 'selector'];

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

try {
  const rows = [];
  for (const { url, data } of PAGES) {
    for (const strategy of STRATEGIES) {
      // A fresh context each time: no cache, so every strategy waits for the same work.
      const context = await browser.newContext();
      const page = await context.newPage();
      const t = Date.now();
      if (strategy === 'selector') {
        await page.goto(url, { waitUntil: 'commit', timeout: 60000 });
        await page.locator(data).first().waitFor({ timeout: 60000 });
      } else {
        await page.goto(url, { waitUntil: strategy, timeout: 60000 });
      }
      const ms = Date.now() - t;
      rows.push({ url, strategy, ms, items_ready: await page.locator(data).count() });
      await context.close();
    }
  }
  console.log(JSON.stringify({ rows }, null, 2));
} finally {
  await browser.close();
}
