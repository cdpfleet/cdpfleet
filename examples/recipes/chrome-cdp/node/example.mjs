// npm install playwright@1.60.0
// cdp: true — Playwright's connectOverCDP (any recent version), or Puppeteer:
//   puppeteer.connect({ browserWSEndpoint: cdpUrl, headers: { 'x-api-key': KEY } })
// Browsers run on cdpfleet, so no `npx playwright install` is needed.
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// 1. Launch the browser
const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({
    proxy: 'http://user:pass@proxy.example.com:8080',
    headless: 'new',
    cdp: true,
  }),
});
if (!res.ok) throw new Error(`launch failed: ${res.status} ${await res.text()}`);
const { cdpUrl } = await res.json();

// 2. Connect and drive it
const browser = await chromium.connectOverCDP(cdpUrl, { headers: { 'x-api-key': KEY } });
const page = await browser.newPage();
await page.goto('https://example.com');
console.log(await page.title());
await browser.close(); // ends the session and stops billing
