// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const URLS = ['https://news.ycombinator.com/', 'https://en.wikipedia.org/wiki/Web_scraping', 'https://books.toscrape.com/'];

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
  const pages = [];
  for (const url of URLS) {
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
    const html = await page.content();
    const text = await page.locator('body').innerText();
    const aria = await page.locator('body').ariaSnapshot();
    pages.push({
      url,
      html_chars: html.length,
      text_chars: text.length,
      aria_chars: aria.length,
      aria_links: (aria.match(/- link /g) || []).length,
      aria_vs_html: Math.round((aria.length / html.length) * 1000) / 10,
      aria_sample: aria.split('\n').slice(0, 6).join('\n'),
    });
  }
  console.log(JSON.stringify({ pages }, null, 2));
} finally {
  await browser.close();
}
