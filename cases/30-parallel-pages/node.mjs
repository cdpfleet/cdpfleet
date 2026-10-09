// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const URLS = [
  'https://books.toscrape.com/',
  'https://quotes.toscrape.com/',
  'https://example.com',
  'https://en.wikipedia.org/wiki/Web_scraping',
  'https://news.ycombinator.com/',
];

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

async function extract(browser, url) {
  const page = await browser.newPage();
  try {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
    return { url, title: await page.title() };
  } finally {
    await page.close();
  }
}

try {
  const t1 = Date.now();
  const sequential = [];
  for (const url of URLS) {
    sequential.push({ ...await extract(browser, url), method: 'sequential' });
  }
  const seqSeconds = Math.round((Date.now() - t1) / 10) / 100;

  const t2 = Date.now();
  const parallel = await Promise.all(URLS.map((url) => extract(browser, url).then((r) => ({ ...r, method: 'parallel' }))));
  const parSeconds = Math.round((Date.now() - t2) / 10) / 100;

  console.log(JSON.stringify({
    sequential_seconds: seqSeconds,
    parallel_seconds: parSeconds,
    speedup: `${Math.round(seqSeconds / parSeconds * 10) / 10}x`,
    results: [...sequential, ...parallel],
  }, null, 2));
} finally {
  await browser.close();
}
