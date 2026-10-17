// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGES = [
  ['example.com', 'https://example.com/'],
  ['books.toscrape.com', 'https://books.toscrape.com/'],
  ['Wikipedia article', 'https://en.wikipedia.org/wiki/Web_browser'],
  ['shifting demo', 'https://shift.test/'], // served by page.route below
];
// A made-up page whose banner arrives late and pushes the text down: a layout shift on purpose.
const SHIFTING = `<!doctype html><meta charset="utf-8"><title>shift</title>
<body style="margin:0;font:18px sans-serif"><div id="slot"></div>
<h1>Article</h1>${'<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>'.repeat(30)}
<script>setTimeout(() => { document.getElementById('slot').innerHTML = '<div style="height:300px;background:#fc0">Late banner</div>'; }, 600);</script>`;

// Reads the vitals from the page's own performance timeline. `buffered: true` returns the
// entries recorded before the observer existed, so this runs after the load.
const VITALS = `new Promise((resolve) => {
  const nav = performance.getEntriesByType('navigation')[0];
  const fcp = performance.getEntriesByName('first-contentful-paint')[0];
  let lcp = null, cls = 0;
  new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e; }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  setTimeout(() => resolve({
    ttfb_ms: Math.round(nav.responseStart),
    connect_ms: Math.round(nav.connectEnd - nav.domainLookupStart), // DNS + TCP + TLS (through the proxy)
    wait_ms: Math.round(nav.responseStart - nav.requestStart), // request sent → first byte
    fcp_ms: fcp ? Math.round(fcp.startTime) : null,
    lcp_ms: lcp ? Math.round(lcp.startTime) : null,
    lcp_element: lcp && lcp.element ? lcp.element.tagName.toLowerCase() : null,
    cls: Math.round(cls * 1000) / 1000,
    load_ms: Math.round(nav.loadEventEnd),
  }), 200);
})`;

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
  for (const [name, url] of PAGES) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 800 } }); // cold cache
    await context.route('https://shift.test/**', (route) => route.fulfill({ contentType: 'text/html; charset=utf-8', body: SHIFTING }));
    const page = await context.newPage();
    // A CDP session over the normal Playwright connection: the browser's own counters.
    const cdp = await context.newCDPSession(page);
    await cdp.send('Performance.enable');
    await page.goto(url, { waitUntil: 'load', timeout: 60000 });
    await page.waitForTimeout(1500); // let late content (and late shifts) land
    const vitals = await page.evaluate(VITALS);
    const metrics = Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map((m) => [m.name, m.value]));
    rows.push({
      page: name, ...vitals,
      script_ms: Math.round(metrics.ScriptDuration * 1000),
      layouts: metrics.LayoutCount,
      dom_nodes: metrics.Nodes,
      js_heap_mb: Math.round(metrics.JSHeapUsedSize / 1048576 * 10) / 10,
    });
    await context.close();
  }
  console.log(JSON.stringify({ rows }, null, 2));
} finally {
  await browser.close();
}
