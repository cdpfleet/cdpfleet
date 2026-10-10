// npm install playwright@1.60.0
// Browsers run on cdpfleet, so no `npx playwright install` is needed.
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// 1. Launch the browser
const res = await fetch('https://starter.cdpfleet.com/yandex/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({
    proxy: 'http://user:pass@proxy.example.com:8080',
    headless: 'new',
  }),
});
if (!res.ok) throw new Error(`launch failed: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();

// 2. Connect and drive it
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const page = await browser.newPage();
await page.goto('https://example.com');
console.log(await page.title());
await browser.close(); // ends the session and stops billing
