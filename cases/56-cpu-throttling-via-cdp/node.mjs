// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RATES = [1, 4, 6];
const URL = 'https://en.wikipedia.org/wiki/Web_browser';
// A fixed amount of JavaScript work, timed inside the page: what a heavy client-side app does.
const WORK = `(() => {
  const t = performance.now();
  let h = 0;
  for (let i = 0; i < 3e6; i++) h = (h * 31 + i) % 1000003;
  const a = Array.from({ length: 200000 }, (_, i) => (i * 7919) % 100003).sort((x, y) => x - y);
  return { ms: Math.round(performance.now() - t), check: h + a[1000] };
})()`;
const metrics = async (cdp) => Object.fromEntries((await cdp.send('Performance.getMetrics')).metrics.map((m) => [m.name, m.value]));

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
  for (const rate of RATES) {
    const context = await browser.newContext();
    const page = await context.newPage();
    // CPU throttling is a DevTools feature: reach it with a CDP session over the Playwright connection.
    const cdp = await context.newCDPSession(page);
    await cdp.send('Performance.enable');
    await cdp.send('Emulation.setCPUThrottlingRate', { rate });
    // A real page load: the network part isn't throttled, the main-thread work is.
    await page.goto(URL, { waitUntil: 'load', timeout: 90000 });
    const nav = await page.evaluate(() => {
      const n = performance.getEntriesByType('navigation')[0];
      return { response_ms: Math.round(n.responseEnd), dcl_ms: Math.round(n.domContentLoadedEventEnd), load_ms: Math.round(n.loadEventEnd) };
    });
    const m = await metrics(cdp);
    const work = await page.evaluate(WORK);
    rows.push({
      rate,
      work_ms: work.ms,
      ...nav,
      parse_ms: nav.dcl_ms - nav.response_ms, // HTML fully received → DOMContentLoaded: parsing and scripts
      main_thread_ms: Math.round(m.TaskDuration * 1000), // busy time while loading, from CDP
      check: work.check,
    });
    await context.close();
  }
  // Is it per page? A throttled tab next to an unthrottled one in the same browser.
  const context = await browser.newContext();
  const slow = await context.newPage();
  const fast = await context.newPage();
  await (await context.newCDPSession(slow)).send('Emulation.setCPUThrottlingRate', { rate: 6 });
  const sideBySide = { throttled_tab_ms: (await slow.evaluate(WORK)).ms, other_tab_ms: (await fast.evaluate(WORK)).ms };
  await context.close();
  console.log(JSON.stringify({ rows, side_by_side: sideBySide }, null, 2));
} finally {
  await browser.close();
}
