// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGE = 'https://en.wikipedia.org/wiki/Web_browser';
const LOADS = 3; // per proxy, each in a fresh context (cold cache, new connections)
const PROXIES = {
  'residential (any country)': process.env.PROXY_URL,
  'residential (Germany)': process.env.PROXY_URL_DE,
  'datacenter SOCKS5': process.env.SOCKS_PROXIES.split(',')[0],
};

// Navigation Timing + Largest Contentful Paint, read in the page after load.
const TIMINGS = () => new Promise((done) => {
  const nav = performance.getEntriesByType('navigation')[0];
  new PerformanceObserver((list) => {
    const lcp = list.getEntries().at(-1);
    done({
      connect: nav.connectEnd - nav.connectStart, // TCP + TLS through the proxy tunnel
      ttfb: nav.responseStart - nav.requestStart,  // first byte after the request was sent
      domReady: nav.domContentLoadedEventEnd,
      load: nav.loadEventEnd,
      lcp: lcp.startTime,
    });
  }).observe({ type: 'largest-contentful-paint', buffered: true });
});

const median = (xs) => [...xs].sort((a, b) => a - b)[Math.floor(xs.length / 2)];

async function measure(label, proxy) {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy, headless: true }),
  });
  if (!res.ok) return { proxy: label, error: `launch ${res.status}` };
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const runs = [];
    for (let i = 0, failures = 0; i < LOADS; i++) {
      const context = await browser.newContext();
      try {
        const page = await context.newPage();
        await page.goto(PAGE, { waitUntil: 'load', timeout: 60000 });
        runs.push(await page.evaluate(TIMINGS));
      } catch (err) {
        if (++failures > 1) throw err; // one failed load is the proxy's noise; two is a problem
        i--;
      } finally {
        await context.close();
      }
    }
    const m = (k) => Math.round(median(runs.map((r) => r[k])));
    return { proxy: label, loads: runs.length, connect_ms: m('connect'), ttfb_ms: m('ttfb'), dom_ready_ms: m('domReady'), lcp_ms: m('lcp'), load_ms: m('load') };
  } finally {
    await browser.close();
  }
}

const rows = await Promise.all(Object.entries(PROXIES).map(([label, proxy]) => measure(label, proxy)));
console.log(JSON.stringify({ page: PAGE, statistic: `median of ${LOADS} cold loads`, results: rows }, null, 2));
