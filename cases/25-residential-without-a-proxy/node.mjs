// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (no proxy of your own needed)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SESSION = `c25${Date.now().toString(36).slice(-6)}`; // a sticky name, 1–10 of [A-Za-z0-9_]

// Four launches, all on the cdpfleet residential proxy — the token is the whole proxy config.
const RUNS = [
  { label: 'rotating, any country', proxy: 'cdpfleet-resi' },
  { label: 'rotating, Germany', proxy: 'cdpfleet-resi-country-de' },
  { label: 'sticky US, browser 1', proxy: `cdpfleet-resi-country-us-session-${SESSION}-lifetime-10` },
  { label: 'sticky US, browser 2', proxy: `cdpfleet-resi-country-us-session-${SESSION}-lifetime-10` },
];

// Residential peers drop a few percent of connections: retry a lookup up to 3 times.
async function lookup(page, i) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await (await page.goto(`http://ip-api.com/json/?fields=query,countryCode&i=${i}-${attempt}`, { timeout: 60000 })).json();
    } catch (err) {
      if (attempt === 3) throw err;
    }
  }
}

async function run({ label, proxy }) {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy, headless: 'new' }),
  });
  if (!res.ok) return { label, error: `launch ${res.status} ${await res.text()}` };
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    // Three lookups on separate connections: rotating exits change, sticky ones don't.
    const exits = [];
    for (let i = 0; i < 3; i++) exits.push(await lookup(page, i));
    return {
      label,
      proxy: proxy.replace(SESSION, '<name>'),
      countries: [...new Set(exits.map((e) => e.countryCode))].join(','),
      distinct_ips: new Set(exits.map((e) => e.query)).size,
      first_ip: exits[0].query,
    };
  } finally {
    await browser.close();
  }
}

const out = [];
for (const r of RUNS) out.push(await run(r)); // in order, so browser 2 starts after browser 1 ended
const [b1, b2] = out.slice(2);
for (const r of out) r.same_ip_as_browser_1 = r.label.startsWith('sticky') ? r.first_ip === b1.first_ip : null;
// The balance the traffic is billed to (metered about once a minute, so it trails a little).
const bal = await (await fetch('https://cdpfleet.com/v1/me/proxy-balance', { headers: { 'x-api-key': KEY } })).json();
for (const r of out) delete r.first_ip;
console.log(JSON.stringify({ runs: out, sticky_ip_kept_across_browsers: b2.same_ip_as_browser_1, balance_allowed: bal.allowed, price_per_gb_usd: bal.price_per_gb_usd }, null, 2));
