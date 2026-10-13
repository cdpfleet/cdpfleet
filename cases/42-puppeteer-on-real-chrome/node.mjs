// npm install puppeteer-core@24
// env: CDPFLEET_API_KEY, PROXY_URL
// Puppeteer on real Google Chrome: launch with "cdp": true and connect to cdpUrl. No browser
// download is needed (puppeteer-core), and the Puppeteer version doesn't have to match anything.
import puppeteer from 'puppeteer-core';

const KEY = process.env.CDPFLEET_API_KEY;
const URL = 'https://books.toscrape.com/';

// Bytes the page pulled over the network, from its own resource timing (fresh context: no cache).
const TRANSFER = `(() => {
  const all = [...performance.getEntriesByType('navigation'), ...performance.getEntriesByType('resource')];
  return { resources: all.length, transfer_kb: Math.round(all.reduce((n, e) => n + (e.transferSize || 0), 0) / 1024) };
})()`;

async function launch() {
  const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new', cdp: true, record: false }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  return res.json();
}

async function isLive(sessionId) {
  const res = await fetch('https://cdpfleet.com/v1/me/threads', { headers: { 'x-api-key': KEY } });
  return (await res.text()).includes(sessionId);
}

async function goto(page) {
  for (let attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
    try { await page.goto(URL, { waitUntil: 'load', timeout: 60000 }); return; } catch (err) { if (attempt === 3) throw err; }
  }
}

// One page in a fresh browser context, optionally aborting image requests.
async function load(browser, blockImages) {
  const context = await browser.createBrowserContext();
  const page = await context.newPage();
  let blocked = 0;
  if (blockImages) {
    await page.setRequestInterception(true);
    page.on('request', (req) => {
      if (req.resourceType() === 'image') { blocked++; req.abort(); } else req.continue();
    });
  }
  const t = Date.now();
  await goto(page);
  const loadMs = Date.now() - t;
  const transfer = await page.evaluate(TRANSFER);
  return { context, page, row: { label: blockImages ? 'images blocked' : 'everything', ...transfer, images_blocked: blocked, load_ms: loadMs } };
}

const s = await launch();
const t = Date.now();
const browser = await puppeteer.connect({ browserWSEndpoint: s.cdpUrl, headers: { 'x-api-key': KEY }, defaultViewport: null });
const connectMs = Date.now() - t;
const out = { client: 'Puppeteer (puppeteer-core 24)', connect_ms: connectMs };
try {
  // Raw CDP at the browser level: what's really on the other end.
  const cdp = await browser.target().createCDPSession();
  const version = await cdp.send('Browser.getVersion');
  out.product = version.product;
  out.protocol_version = version.protocolVersion;
  const pages = async () => (await cdp.send('Target.getTargets')).targetInfos.filter((x) => x.type === 'page').length;
  const before = await pages();

  const full = await load(browser, false);
  const lean = await load(browser, true);
  out.targets = { pages_before: before, pages_after: await pages() };
  out.loads = [full.row, lean.row];

  // Puppeteer wraps Performance.getMetrics as page.metrics().
  const m = await full.page.metrics();
  out.metrics = { dom_nodes: m.Nodes, js_heap_used_mb: Math.round(m.JSHeapUsedSize / 1048576 * 10) / 10 };
  await full.context.close();
  await lean.context.close();
} finally {
  await browser.close(); // closes the connection, which ends the cdpfleet session
}
const closed = Date.now();
while (await isLive(s.sessionId)) {
  if (Date.now() - closed > 60000) break;
  await new Promise((r) => setTimeout(r, 1000));
}
out.session_ended_s = (await isLive(s.sessionId)) ? null : Math.round((Date.now() - closed) / 100) / 10;
console.log(JSON.stringify(out, null, 2));
