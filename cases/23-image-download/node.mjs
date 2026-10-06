// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGE = 'https://books.toscrape.com/'; // 20 cover images

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

const isJpeg = (buf) => buf.length > 3 && buf[0] === 0xff && buf[1] === 0xd8 && buf[2] === 0xff;
const summary = (method, files, seconds, extra_requests) => ({
  method, images: files.length, valid_jpeg: files.filter((f) => isJpeg(f.bytes)).length,
  total_kb: Math.round(files.reduce((n, f) => n + f.bytes.length, 0) / 1024), extra_requests, seconds,
});

try {
  // Way 1: keep the bytes the page downloads anyway — zero extra requests.
  const page = await browser.newPage();
  const captured = [];
  page.on('response', async (r) => {
    if (r.request().resourceType() === 'image' && r.ok()) {
      try { captured.push({ url: r.url(), bytes: await r.body() }); } catch { /* body gone (cache) */ }
    }
  });
  const t1 = Date.now();
  await page.goto(PAGE, { timeout: 60000, waitUntil: 'networkidle' });
  const covers = await page.$$eval('article.product_pod img', (imgs) => imgs.map((i) => i.currentSrc || i.src));
  const fromLoad = captured.filter((c) => covers.includes(c.url));
  const way1 = summary('capture responses while the page loads', fromLoad, (Date.now() - t1) / 1000, 0);

  // Way 2: fetch each image again from inside the page (same cookies, proxy and headers as
  // the page itself) and hand the bytes to the client as base64.
  const t2 = Date.now();
  const again = await page.evaluate((urls) => Promise.all(urls.map(async (url) => {
    const buf = await (await fetch(url, { cache: 'no-store' })).arrayBuffer();
    let s = ''; const b = new Uint8Array(buf); for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
    return { url, b64: btoa(s) };
  })), covers);
  const refetched = again.map((a) => ({ url: a.url, bytes: Buffer.from(a.b64, 'base64') }));
  const way2 = summary('fetch() each image again in the page', refetched, (Date.now() - t2) / 1000, covers.length);

  console.log(JSON.stringify([way1, way2], null, 2));
} finally {
  await browser.close();
}
