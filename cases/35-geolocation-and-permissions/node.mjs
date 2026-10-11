// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const BERLIN = { latitude: 52.52, longitude: 13.405 };

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL_DE, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// What a page can learn about where the visitor is, from JavaScript.
const PROBE = `async () => {
  const geo = await new Promise((resolve) => navigator.geolocation.getCurrentPosition(
    (p) => resolve({ lat: Math.round(p.coords.latitude * 100) / 100, lon: Math.round(p.coords.longitude * 100) / 100 }),
    (e) => resolve({ error: e.code === 1 ? 'permission denied' : e.message }),
    { timeout: 5000 }));
  const perm = (await navigator.permissions.query({ name: 'geolocation' })).state;
  return { geolocation: geo, permission: perm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, language: navigator.language, utc_offset_min: -new Date().getTimezoneOffset() };
}`;

try {
  const out = { ip: null, contexts: [] };
  const setups = [
    ['defaults', {}],
    ['permission granted, no position', { permissions: ['geolocation'] }],
    ['Berlin: geolocation + timezone + locale', { permissions: ['geolocation'], geolocation: BERLIN, timezoneId: 'Europe/Berlin', locale: 'de-DE' }],
  ];
  for (const [label, options] of setups) {
    const ctx = await browser.newContext(options);
    const page = await ctx.newPage();
    if (!out.ip) {
      await page.goto('http://ip-api.com/json/?fields=query,countryCode,timezone', { timeout: 60000 });
      const ip = JSON.parse(await page.textContent('body'));
      out.ip = { country: ip.countryCode, timezone: ip.timezone };
    }
    await page.goto('https://httpbin.org/html', { timeout: 60000 });
    out.contexts.push({ setup: label, ...(await page.evaluate(`(${PROBE})()`)) });
    await ctx.close();
  }
  console.log(JSON.stringify(out, null, 2));
} finally {
  await browser.close();
}
