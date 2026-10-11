// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';
import { mkdtempSync, statSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const URL = 'https://books.toscrape.com/';

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const dir = mkdtempSync(path.join(tmpdir(), 'har-'));
const har = path.join(dir, 'books.har');

try {
  // 1. Record: every request of the page goes into a HAR file on this machine.
  const rec = await browser.newContext({ recordHar: { path: har, mode: 'minimal' } });
  const p1 = await rec.newPage();
  let t = Date.now();
  await p1.goto(URL, { waitUntil: 'load', timeout: 60000 });
  const liveMs = Date.now() - t;
  const liveBooks = await p1.locator('article.product_pod').count();
  await rec.close(); // writes the HAR
  const entries = JSON.parse(readFileSync(har, 'utf8')).log.entries;

  // 2. Replay: serve every request from the HAR; anything not in it is refused, so the
  //    page can't quietly reach the network.
  const rep = await browser.newContext();
  let fromHar = 0;
  let refused = 0;
  rep.on('requestfinished', () => { fromHar++; });
  rep.on('requestfailed', () => { refused++; });
  await rep.routeFromHAR(har, { notFound: 'abort' });
  const p2 = await rep.newPage();
  t = Date.now();
  await p2.goto(URL, { waitUntil: 'load', timeout: 60000 });
  const replayMs = Date.now() - t;
  const replayBooks = await p2.locator('article.product_pod').count();
  // A page the HAR doesn't have: refused instead of fetched.
  const missing = await p2.goto('https://books.toscrape.com/catalogue/page-2.html').then(() => 'loaded', (e) => (/ERR_FAILED|aborted/i.test(e.message) ? 'refused' : e.message.split('\n')[0]));
  await rep.close();

  console.log(JSON.stringify({
    har_entries: entries.length,
    har_kb: Math.round(statSync(har).size / 1024),
    live: { ms: liveMs, books: liveBooks },
    replay: { ms: replayMs, books: replayBooks, served_from_har: fromHar, refused },
    page_not_in_har: missing,
  }, null, 2));
} finally {
  await browser.close();
  rmSync(dir, { recursive: true, force: true });
}
