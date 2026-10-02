// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// The live catalog says which channels and previous majors exist right now.
const catalog = await (await fetch('https://cdpfleet.com/api/public/browsers')).json();
const versions = catalog.engines.find((e) => e.key === 'chrome').versions;
const variants = versions.map((v) => (v.label === 'pinned'
  ? { label: `version ${v.version.split('.')[0]}`, options: { version: v.version.split('.')[0] }, catalog: v.version }
  : { label: `channel ${v.label}`, options: v.label === 'stable' ? {} : { channel: v.label }, catalog: v.version }));

async function probe({ label, options, catalog: expected }) {
  const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    // headless: false (a real display) so the user agent doesn't say "HeadlessChrome".
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: false, ...options }),
  });
  if (!res.ok) return { variant: label, error: `${res.status} ${await res.text()}` };
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    // A residential exit occasionally times out: one retry, in a fresh tab.
    const load = async () => (await browser.newPage()).goto('https://tls.peet.ws/api/all', { timeout: 30000 });
    const fp = await (await load().catch(load)).json();
    const headers = fp.http2.sent_frames.find((f) => f.frame_type === 'HEADERS').headers;
    return {
      variant: label,
      catalog_version: expected,
      browser_version: browser.version(),
      user_agent: fp.user_agent,
      sec_ch_ua: headers.find((h) => h.startsWith('sec-ch-ua: '))?.slice(11) ?? null,
      ja4: fp.tls.ja4,
      akamai_h2_hash: fp.http2.akamai_fingerprint_hash,
    };
  } finally {
    await browser.close();
  }
}

// All variants at once: each is its own session.
console.log(JSON.stringify(await Promise.all(variants.map(probe)), null, 2));
