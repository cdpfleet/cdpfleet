// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (http://user:pass@host:port)
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;

// One browser per engine family. `family` is the Playwright client that speaks to it.
const BROWSERS = [
  { name: 'chrome', family: chromium },
  { name: 'edge', family: chromium },
  { name: 'firefox', family: firefox },
  { name: 'camoufox', family: firefox },
  { name: 'webkit', family: webkit },
];

async function launch(name, options) {
  const res = await fetch(`https://starter.cdpfleet.com/${name}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify(options),
  });
  if (!res.ok) throw new Error(`launch ${name}: ${res.status} ${await res.text()}`);
  return res.json();
}

// Navigate the browser itself to tls.peet.ws: the JSON it returns describes the TLS
// ClientHello and HTTP/2 frames this very browser sent. (page.request would go out from
// Playwright's own HTTP client instead, with a different fingerprint.)
// A residential exit occasionally times out: one retry, in a fresh tab.
async function fingerprint(browser) {
  for (let attempt = 1; ; attempt++) {
    try {
      const page = await browser.newPage();
      return await (await page.goto('https://tls.peet.ws/api/all', { timeout: 30000 })).json();
    } catch (err) {
      if (attempt === 2) throw err;
    }
  }
}

const rows = [];
for (const b of BROWSERS) {
  const session = await launch(b.name, { proxy: PROXY, headless: true });
  const browser = await b.family.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const fp = await fingerprint(browser);
    rows.push({
      browser: b.name,
      version: browser.version(),
      user_agent: fp.user_agent,
      http_version: fp.http_version,
      ja4: fp.tls.ja4,
      ja3_hash: fp.tls.ja3_hash,
      peetprint_hash: fp.tls.peetprint_hash,
      akamai_h2: fp.http2?.akamai_fingerprint ?? null,
      akamai_h2_hash: fp.http2?.akamai_fingerprint_hash ?? null,
      cipher_suites: fp.tls.ciphers.length,
      extensions: fp.tls.extensions.length,
    });
  } finally {
    await browser.close(); // ends the session and stops billing
  }
}
console.log(JSON.stringify(rows, null, 2));
