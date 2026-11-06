// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY
import { randomBytes } from 'node:crypto';
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// The same twelve setups as the real-site lab cases 70-77 (Rebrowser needs a 1.52 client, so it's left out).
const SETUPS = [
  ['chrome_headful', 'Chrome, headful', 'chrome', chromium, false],
  ['chrome_headless', 'Chrome, headless', 'chrome', chromium, 'new'],
  ['edge', 'Edge, headful', 'edge', chromium, false],
  ['brave', 'Brave, headful', 'brave', chromium, false],
  ['opera', 'Opera, headful', 'opera', chromium, false],
  ['yandex', 'Yandex, headful', 'yandex', chromium, false],
  ['whale', 'Whale, headful', 'whale', chromium, false],
  ['chromium_headless', 'Chromium, headless', 'chromium', chromium, true],
  ['patchright', 'Patchright, headful', 'patchright', chromium, false],
  ['firefox', 'Firefox, headful', 'firefox', firefox, false],
  ['webkit', 'WebKit, headful', 'webkit', webkit, false],
  ['camoufox', 'Camoufox, headful', 'camoufox', firefox, false],
];

async function launch(engine, headless, proxy) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy, headless, record: false }),
    });
    if (res.ok) return (await res.json()).wsUrl;
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// The browser itself loads a TLS echo page, which answers with the ClientHello and HTTP/2 frames
// it received from this very browser. tls.peet.ws first; tls.browserleaks.com/json only if peet
// doesn't answer (it has had outages). Each is one page load in a fresh tab.
const ECHOES = [
  ['tls.peet.ws', 'https://tls.peet.ws/api/all', (j) => ({ http_version: j.http_version, ja4: j.tls?.ja4, ja4_r: j.tls?.ja4_r ?? null, peetprint_hash: j.tls?.peetprint_hash ?? null, akamai_h2: j.http2?.akamai_fingerprint ?? null, akamai_h2_hash: j.http2?.akamai_fingerprint_hash ?? null, user_agent: j.user_agent })],
  ['tls.browserleaks.com', 'https://tls.browserleaks.com/json', (j) => ({ http_version: null, ja4: j.ja4, ja4_r: j.ja4_r || null, peetprint_hash: null, akamai_h2: j.akamai_text || null, akamai_h2_hash: j.akamai_hash || null, user_agent: j.user_agent })],
];

async function fingerprint(browser) {
  const context = await browser.newContext();
  let last = null;
  for (const [source, url, read] of ECHOES) {
    const tab = await context.newPage();
    try {
      const res = await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
      const fp = read(JSON.parse(await res.text()));
      if (fp.ja4) return { source, ...fp };
      last = `${source}: no ja4 in the answer`;
    } catch (err) {
      last = `${source}: ${err.message.split('\n')[0]}`;
    } finally {
      await tab.close().catch(() => {});
    }
  }
  throw new Error(last);
}

// One sticky residential exit for the whole run, and one session at a time.
const proxy = `cdpfleet-resi-country-us-session-c84${RUN}-lifetime-30`;
const fingerprints = {};
for (const [id, , engine, type, headless] of SETUPS) {
  try {
    const browser = await type.connect(await launch(engine, headless, proxy), { headers: { 'x-api-key': KEY } });
    try {
      fingerprints[id] = { version: browser.version(), ...(await fingerprint(browser)) };
    } finally {
      await browser.close();
    }
  } catch (err) {
    fingerprints[id] = { error: err.message.split('\n')[0].slice(0, 200) };
  }
}
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), setups: Object.fromEntries(SETUPS.map((s) => [s[0], s[1]])), fingerprints }, null, 2));
