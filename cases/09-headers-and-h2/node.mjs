// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: false }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// What the server saw: header names in order, and the HTTP/2 fingerprint.
async function observe(label, contextOptions, setup) {
  const context = await browser.newContext(contextOptions);
  const page = await context.newPage();
  if (setup) await setup(page);
  const fp = await (await page.goto('https://tls.peet.ws/api/all', { timeout: 60000 })).json();
  await context.close();
  const headers = fp.http2.sent_frames.find((f) => f.frame_type === 'HEADERS').headers;
  return {
    variant: label,
    header_order: headers.map((h) => h.slice(0, h.indexOf(':', 1))),
    accept_language: headers.find((h) => h.startsWith('accept-language: '))?.slice(17) ?? null,
    akamai_h2: fp.http2.akamai_fingerprint,
    ja4: fp.tls.ja4,
  };
}

try {
  const rows = [
    await observe('default', {}),
    await observe('locale: de-DE', { locale: 'de-DE' }),
    await observe('extraHTTPHeaders', { extraHTTPHeaders: { 'accept-language': 'de-DE', 'x-request-id': 'abc123' } }),
    await observe('route: rewrite headers', {}, (page) => page.route('**/*', (route) => route.continue({
      headers: { ...route.request().headers(), 'x-request-id': 'abc123' },
    }))),
  ];
  console.log(JSON.stringify(rows, null, 2));
} finally {
  await browser.close();
}
