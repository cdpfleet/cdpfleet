// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const URL = 'https://books.toscrape.com/';
// Chrome DevTools' presets (throughput in bytes/s, latency in ms).
const PROFILES = {
  'no throttling': null,
  'Fast 4G': { latency: 150, downloadThroughput: (9 * 1024 * 1024) / 8, uploadThroughput: (1.5 * 1024 * 1024) / 8 },
  'Slow 3G': { latency: 2000, downloadThroughput: (400 * 1024) / 8, uploadThroughput: (400 * 1024) / 8 },
};

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
  for (const [profile, conditions] of Object.entries(PROFILES)) {
    const context = await browser.newContext(); // no cache between runs
    const page = await context.newPage();
    // A CDP session works over the normal Playwright connection on Chromium-based browsers.
    const cdp = await context.newCDPSession(page);
    await cdp.send('Network.enable');
    if (conditions) await cdp.send('Network.emulateNetworkConditions', { offline: false, ...conditions });
    const t = Date.now();
    await page.goto(URL, { waitUntil: 'domcontentloaded', timeout: 120000 });
    const htmlMs = Date.now() - t;
    await page.waitForLoadState('load', { timeout: 180000 });
    rows.push({ profile, html_ms: htmlMs, load_ms: Date.now() - t });
    await context.close();
  }
  // Offline: the page fails like it would on a dead connection.
  const context = await browser.newContext();
  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: true, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  const offline = await page.goto(URL, { timeout: 30000 }).then(() => 'loaded', (e) => (e.message.match(/net::[A-Z_]+/) || [e.message.split('\n')[0]])[0]);
  await context.close();
  console.log(JSON.stringify({ rows, offline }, null, 2));
} finally {
  await browser.close();
}
