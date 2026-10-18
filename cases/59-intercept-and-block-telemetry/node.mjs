// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITES = [
  { name: 'The Guardian', url: 'https://www.theguardian.com/international' },
  { name: 'NPR', url: 'https://www.npr.org/' },
  { name: 'Wikipedia', url: 'https://en.wikipedia.org/wiki/Web_browser' },
];
// Analytics, measurement and real-user-monitoring hosts (subdomains included) — not ads.
const TELEMETRY = ['google-analytics.com', 'analytics.google.com', 'googletagmanager.com', 'ophan.theguardian.com',
  'performance-events.guardianapis.com', 'feast-events.guardianapis.com', 'scorecardresearch.com', 'permutive.app',
  'permutive.com', 'go-mpulse.net', 'akstat.io', 'nr-data.net', 'newrelic.com', 'sentry.io', 'datadoghq.com',
  'hotjar.com', 'clarity.ms', 'segment.io', 'segment.com', 'chartbeat.com', 'chartbeat.net', 'parsely.com',
  'analytics.yahoo.com', 'omtrdc.net', 'demdex.net', 'bat.bing.com', 'cloudflareinsights.com', 'brandmetrics.com'];

// A request is telemetry if it goes to one of those hosts or is a beacon (navigator.sendBeacon, <a ping>).
const isTelemetry = (req) => {
  if (req.resourceType() === 'ping') return true;
  const host = new URL(req.url()).hostname;
  return TELEMETRY.some((d) => host === d || host.endsWith(`.${d}`));
};

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL_US, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// One load in a fresh context: DOM ready + 6 s, then leave the page (about:blank) and give the
// page-hide beacons 1.5 s. Counts every request, the telemetry among them, and bytes on the wire.
async function load(site, block) {
  const context = await browser.newContext();
  let blocked = 0;
  if (block) {
    await context.route('**/*', (route) => {
      if (isTelemetry(route.request())) { blocked++; return route.abort(); }
      return route.continue();
    });
  }
  const page = await context.newPage();
  const m = { requests: 0, telemetry: 0, onLeave: 0, bytes: 0, telemetryBytes: 0 };
  let leaving = false;
  page.on('request', (req) => {
    m.requests++;
    if (isTelemetry(req)) { m.telemetry++; if (leaving) m.onLeave++; }
  });
  const sizing = []; // sizes() is async: wait for them all before closing the context
  page.on('requestfinished', (req) => {
    sizing.push(req.sizes().then((s) => {
      const n = s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
      m.bytes += n;
      if (isTelemetry(req)) m.telemetryBytes += n;
    }).catch(() => {}));
  });
  const t = Date.now();
  await page.goto(site.url, { waitUntil: 'domcontentloaded', timeout: 90000 });
  const domReady = Date.now() - t;
  await page.waitForTimeout(6000);
  const title = await page.title();
  const headings = await page.locator('h1, h2, h3').count(); // the content is still there
  leaving = true;
  await page.goto('about:blank');
  await page.waitForTimeout(1500);
  await Promise.all(sizing);
  await context.close();
  return { ...m, blocked, domReady, title, headings };
}

try {
  const rows = [];
  for (const site of SITES) {
    const off = await load(site, false);
    const on = await load(site, true);
    rows.push({
      site: site.name,
      title_ok: Boolean(off.title) && off.title === on.title,
      requests: off.requests,
      telemetry_requests: off.telemetry,
      telemetry_on_leave: off.onLeave,
      telemetry_kb: Math.round(off.telemetryBytes / 1024),
      total_kb: Math.round(off.bytes / 1024),
      blocked: on.blocked,
      headings: off.headings,
      headings_blocked_run: on.headings,
      dom_ready_ms: off.domReady,
      dom_ready_ms_blocked_run: on.domReady,
    });
  }
  console.log(JSON.stringify(rows, null, 2));
} finally {
  await browser.close();
}
