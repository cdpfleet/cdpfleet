// npm install playwright@1.60.0
// Browsers run on cdpfleet, so no `npx playwright install` is needed.
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// 1. Launch the browser
const res = await fetch('https://starter.cdpfleet.com/camoufox/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({
    proxy: 'http://user:pass@proxy.example.com:8080',
    locale: 'en-GB',
    fingerprint: {
      navigator: {
        userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:152.0) Gecko/20100101 Firefox/152.0',
        platform: 'MacIntel',
        hardwareConcurrency: 8,
        maxTouchPoints: 0,
      },
      screen: {
        width: 1512,
        height: 982,
        availWidth: 1512,
        availHeight: 944,
        colorDepth: 30,
        pixelDepth: 30,
      },
    },
  }),
});
if (!res.ok) throw new Error(`launch failed: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();

// 2. Connect and drive it
const browser = await firefox.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const page = await browser.newPage();
await page.goto('https://example.com');
console.log(await page.title());
await browser.close(); // ends the session and stops billing
