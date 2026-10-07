// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// What a page can notice about the client driving it. A debugger that has Runtime.enable'd
// the page serializes logged errors, which reads their `stack` getter.
const PROBE = `(async () => {
  let stackRead = false;
  const e = new Error('probe');
  Object.defineProperty(e, 'stack', { get() { stackRead = true; return ''; } });
  console.debug(e);
  await new Promise((r) => setTimeout(r, 100));
  return { stack_read_by_debugger: stackRead, webdriver: navigator.webdriver };
})()`;

async function launch(cdp) {
  const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new', cdp }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  return res.json();
}

async function measure(mode) {
  const s = await launch(mode !== 'playwright protocol');
  const t = Date.now();
  const browser = mode === 'playwright protocol'
    ? await chromium.connect(s.wsUrl, { headers: { 'x-api-key': KEY } })
    : await chromium.connectOverCDP(s.cdpUrl, { headers: { 'x-api-key': KEY } });
  const connect_ms = Date.now() - t;
  try {
    const page = await browser.newPage();
    const logged = [];
    page.on('console', (m) => logged.push(m.type()));
    for (let attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
      try { await page.goto('https://example.com/', { timeout: 60000 }); break; } catch (err) { if (attempt === 3) throw err; }
    }
    const seen = await page.evaluate(PROBE);
    let pdf = false;
    try { pdf = (await page.pdf()).length > 0; } catch { /* not over this connection */ }
    return {
      mode,
      endpoint: mode === 'playwright protocol' ? 'wsUrl' : 'cdpUrl',
      client_version_must_match: mode === 'playwright protocol',
      connect_ms,
      browser_version: browser.version(),
      console_events: logged.includes('debug'),
      pdf,
      ...seen,
    };
  } finally {
    await browser.close();
  }
}

console.log(JSON.stringify([await measure('playwright protocol'), await measure('connectOverCDP')], null, 2));
