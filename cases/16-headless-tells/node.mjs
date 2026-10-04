// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;

// Four ways to run a browser; the same twelve signals read from inside the page.
const TARGETS = [
  { label: 'Chromium, headless', engine: 'chromium', type: chromium, body: { headless: 'new' } },
  { label: 'Chromium, headful', engine: 'chromium', type: chromium, body: { headless: false } },
  { label: 'Patchright, headful', engine: 'patchright', type: chromium, body: { headless: false } },
  // Camoufox: read the page's own world ("mw:" needs main_world_eval), like a site would.
  { label: 'Camoufox, headful', engine: 'camoufox', type: firefox, body: { headless: false, os: 'windows', main_world_eval: true }, prefix: 'mw:' },
];

// The classic headless and automation tells, read the way a detection script reads them.
const SIGNALS = `(async () => {
  let query = null;
  try { query = (await navigator.permissions.query({ name: 'notifications' })).state; } catch { query = 'error'; }
  const gl = (() => {
    try { const g = document.createElement('canvas').getContext('webgl'); const d = g.getExtension('WEBGL_debug_renderer_info'); return g.getParameter(d ? d.UNMASKED_RENDERER_WEBGL : g.RENDERER); } catch { return null; }
  })();
  return {
    ua_says_headless: /Headless/.test(navigator.userAgent),
    webdriver: navigator.webdriver,
    plugins: navigator.plugins.length,
    notification_mismatch: typeof Notification !== 'undefined' && Notification.permission === 'denied' && query === 'prompt',
    outer_window_zero: outerWidth === 0 || outerHeight === 0,
    screen: screen.width + 'x' + screen.height,
    webgl_renderer: gl,
    cores: navigator.hardwareConcurrency,
    memory_gb: navigator.deviceMemory ?? null,
    languages: navigator.languages.join(','),
    chrome_object: typeof window.chrome === 'object' && window.chrome !== null,
  };
})()`;

async function inspect({ label, engine, type, body, prefix = '' }) {
  const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: PROXY, ...body }),
  });
  if (!res.ok) return { label, error: `launch ${res.status} ${await res.text()}` };
  const { wsUrl, weight } = await res.json();
  const browser = await type.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    await page.goto('https://example.com/', { timeout: 60000 });
    const signals = await page.evaluate(prefix + SIGNALS);
    const tells = ['ua_says_headless', 'webdriver', 'notification_mismatch', 'outer_window_zero'].filter((k) => signals[k]);
    return { label, threads: weight, ...signals, tells: tells.length ? tells.join(', ') : 'none' };
  } finally {
    await browser.close();
  }
}

console.log(JSON.stringify(await Promise.all(TARGETS.map(inspect)), null, 2));
