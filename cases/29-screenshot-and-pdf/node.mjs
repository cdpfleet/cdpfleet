// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';
import { stat, unlink, mkdir } from 'fs/promises';
import { tmpdir } from 'os';
import { join } from 'path';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGE = 'https://books.toscrape.com/';

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

const dir = join(tmpdir(), `cdpfleet-captures-${Date.now()}`);
await mkdir(dir, { recursive: true });

async function capture(label, fn) {
  const t = Date.now();
  const path = join(dir, label.replace(/\s+/g, '-') + (label.includes('pdf') ? '.pdf' : '.png'));
  await fn(path);
  const { size } = await stat(path);
  await unlink(path);
  return { type: label, file_size_bytes: size, seconds: Math.round((Date.now() - t) / 10) / 100 };
}

try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  await page.goto(PAGE, { timeout: 60000, waitUntil: 'networkidle' });

  const results = [];
  results.push(await capture('viewport screenshot', (p) => page.screenshot({ path: p })));
  results.push(await capture('full-page screenshot', (p) => page.screenshot({ path: p, fullPage: true })));
  results.push(await capture('element screenshot', (p) => page.locator('.product_pod').first().screenshot({ path: p })));
  results.push(await capture('pdf', (p) => page.pdf({ path: p })));

  console.log(JSON.stringify({ captures: results }, null, 2));
} finally {
  await browser.close();
}
