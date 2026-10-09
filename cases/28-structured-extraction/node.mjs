// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const START = 'https://books.toscrape.com/';
const STARS = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };

const EXTRACT_JS = `() => [...document.querySelectorAll('article.product_pod')].map(el => {
  const stars = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };
  const ratingClass = [...el.querySelector('.star-rating').classList].find(c => c !== 'star-rating');
  return {
    title: el.querySelector('h3 a').getAttribute('title'),
    price: parseFloat(el.querySelector('.price_color').textContent.replace(/[^0-9.]/g, '')),
    rating: stars[ratingClass] || 0,
    in_stock: el.querySelector('.availability').textContent.trim().toLowerCase().includes('in stock'),
  };
})`;

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

try {
  const page = await browser.newPage();
  const t = Date.now();
  const books = [];

  await page.goto(START, { timeout: 60000 });
  books.push(...await page.evaluate(`(${EXTRACT_JS})()`));

  const next = page.locator('li.next a');
  if (await next.count() > 0) {
    await next.click();
    await page.waitForLoadState('domcontentloaded');
    books.push(...await page.evaluate(`(${EXTRACT_JS})()`));
  }

  console.log(JSON.stringify({
    pages_scraped: 2,
    total_books: books.length,
    books,
    seconds: Math.round((Date.now() - t) / 10) / 100,
  }, null, 2));
} finally {
  await browser.close();
}
