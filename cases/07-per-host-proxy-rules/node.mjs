// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs, 3 or more)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const [socksA, socksB, socksC] = process.env.SOCKS_PROXIES.split(',');
const hostOf = (url) => new URL(url.replace(/^socks5h?:/, 'http:')).hostname;

const options = {
  proxy: process.env.PROXY_URL, // everything not matched below
  proxy_rules: [
    { hosts: ['*.ident.me', 'ident.me'], proxy: socksA },
    { hosts: ['httpbin.org', '*.httpbin.org'], proxy: [socksB, socksC] }, // round-robin
    // IP-lookup services always use the default proxy, so this rule is ignored on purpose.
    { hosts: ['api.ipify.org'], proxy: socksA },
  ],
  headless: true,
};

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify(options),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// Each context has its own connection pool, so each reading is a fresh connection.
// Proxies drop a connection now and then: one retry.
async function exitIpVia(url) {
  for (let attempt = 1; ; attempt++) {
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      const text = await (await page.goto(url, { timeout: 30000 })).text();
      return text.match(/\d{1,3}(\.\d{1,3}){3}/)?.[0] ?? `(no IP in ${url})`;
    } catch (err) {
      if (attempt === 2) return `(failed: ${err.message.split('\n')[0]})`;
    } finally {
      await context.close();
    }
  }
}

try {
  const readings = [];
  for (const url of ['https://v4.ident.me/', 'https://httpbin.org/ip', 'https://httpbin.org/ip', 'https://httpbin.org/ip', 'https://api.ipify.org/', 'https://www.cloudflare.com/cdn-cgi/trace']) {
    readings.push({ url, exit_ip: await exitIpVia(url) });
  }
  console.log(JSON.stringify({
    rules: {
      '*.ident.me': hostOf(socksA),
      'httpbin.org': [hostOf(socksB), hostOf(socksC)],
      'api.ipify.org': `${hostOf(socksA)} (ignored: IP-lookup host)`,
      '(everything else)': 'residential PROXY_URL',
    },
    readings,
  }, null, 2));
} finally {
  await browser.close();
}
