// npm install playwright@1.60.0
// Browsers run on cdpfleet, so no `npx playwright install` is needed.
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// 1. Launch the browser
const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({
    headless: 'new',
    proxy: [
      'http://u:p@res1.example:8080',
      'http://u:p@res2.example:8080',
    ],
    proxy_rules: [
      {
        name: 'dc',
        hosts: [
          '*.cloudfront.net',
        ],
        proxy: 'http://u:p@dc.example:4444',
      },
    ],
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
