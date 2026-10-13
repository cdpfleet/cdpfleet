// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;

// Six ways to drive a remote browser. Chromium (Playwright's build) and Patchright share a
// Chromium base, so they're the like-for-like pair; Chrome is also driven over CDP, with and
// without a console listener; Camoufox is Firefox, driven over Playwright's Juggler, not CDP.
const SETUPS = [
  { label: 'Chromium, Playwright connect', engine: 'chromium', type: chromium, cdp: false, listen: true },
  { label: 'Patchright, Playwright connect', engine: 'patchright', type: chromium, cdp: false, listen: true },
  { label: 'Chrome, Playwright connect', engine: 'chrome', type: chromium, cdp: false, listen: true },
  { label: 'Chrome, connectOverCDP', engine: 'chrome', type: chromium, cdp: true, listen: true },
  { label: 'Chrome, connectOverCDP, no console listener', engine: 'chrome', type: chromium, cdp: true, listen: false },
  { label: 'Camoufox, Playwright connect', engine: 'camoufox', type: firefox, cdp: false, listen: true },
];

// What a detection script can do from inside the page, run as the page's own script:
//  - the classic tell: log an Error whose `stack` is a getter; a debugger client that previews
//    logged objects used to read it;
//  - an Error whose `message` is a getter, the same idea;
//  - the cost of console.log: with a client listening to the console, every logged object is
//    serialized for it. Logging a big object 40 times is timed against JSON.stringify of the
//    same object (so the machine's speed cancels out), five times; the median ratio is kept.
// The verdict is left in an attribute that every client can read.
const PAGE = `<html><body><p>console probe</p><script>
  const out = { stack_getter_read: false, message_getter_read: false };
  const e1 = new Error('probe');
  Object.defineProperty(e1, 'stack', { get() { out.stack_getter_read = true; return ''; } });
  console.debug(e1);
  const e2 = new Error('probe');
  Object.defineProperty(e2, 'message', { get() { out.message_getter_read = true; return ''; } });
  console.log(e2);
  console.log('hello-from-page');
  const big = Array.from({ length: 1000 }, (_, i) => ({ i, s: 'x'.repeat(40), a: [1, 2, 3, { d: i }] }));
  const ratios = [];
  for (let k = 0; k < 5; k++) {
    let t = performance.now();
    for (let i = 0; i < 40; i++) console.log(big);
    const logMs = performance.now() - t;
    t = performance.now();
    for (let i = 0; i < 40; i++) JSON.stringify(big);
    ratios.push(logMs / (performance.now() - t));
  }
  ratios.sort((a, b) => a - b);
  out.log_cost_ratio = Math.round(ratios[2] * 100) / 100;
  setTimeout(() => document.documentElement.setAttribute('data-out', JSON.stringify(out)), 1000);
</script></body></html>`;

async function run({ label, engine, type, cdp, listen }) {
  const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: PROXY, headless: true, record: false, ...(cdp ? { cdp: true } : {}) }),
  });
  if (!res.ok) return { label, error: `launch ${res.status} ${await res.text()}` };
  const { wsUrl, cdpUrl } = await res.json();
  const browser = cdp
    ? await type.connectOverCDP(cdpUrl, { headers: { 'x-api-key': KEY } })
    : await type.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const context = cdp ? browser.contexts()[0] : await browser.newContext();
    const page = await context.newPage();
    let consoleEvents = 0;
    if (listen) page.on('console', (m) => { if (m.text().includes('hello-from-page')) consoleEvents++; });
    await page.setContent(PAGE);
    await page.waitForSelector('html[data-out]', { timeout: 30000 });
    const probe = JSON.parse(await page.evaluate(() => document.documentElement.getAttribute('data-out')));
    return {
      label, engine, connect: cdp ? 'connectOverCDP' : 'connect', console_listener: listen,
      console_events: listen ? consoleEvents : null, ...probe,
      evaluate_works: (await page.evaluate(() => 6 * 7)) === 42,
    };
  } finally {
    await browser.close();
  }
}

const setups = [];
for (const s of SETUPS) setups.push(await run(s)); // one at a time: an idle session ends after 60 s
console.log(JSON.stringify({ setups }, null, 2));
