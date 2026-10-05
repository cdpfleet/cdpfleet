// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
// Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4).
const PEET = 'https://tls.peet.ws/api/all';

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

async function row(path, seen, runs_on) {
  const ip = seen.ip.split(':')[0];
  // Who owns the exit: a residential ISP (your proxy) or a hosting provider (a server)?
  const who = await (await fetch(`http://ip-api.com/json/${ip}?fields=hosting`)).json();
  return {
    path, runs_on, exit_ip: ip, exit_type: who.hosting ? 'datacenter' : 'residential',
    user_agent: seen.user_agent, http_version: seen.http_version, ja4: seen.tls.ja4,
    tls_extensions: seen.tls.extensions.length,
    resumed_tls: seen.tls.extensions.some((e) => /pre_shared_key/.test(e.name)),
  };
}

try {
  const page = await browser.newPage();
  // 1. A navigation: the browser itself makes the request.
  const nav = await (await page.goto(PEET, { timeout: 60000 })).json();
  // 2. fetch() inside the page (same origin): the browser's network stack, cookies and headers.
  const inPage = await page.evaluate(() => fetch('/api/all').then((r) => r.json()));
  // 3. page.request: Playwright's own HTTP client, run by the Playwright server next to the browser.
  const viaRequest = await (await page.request.get(PEET, { timeout: 60000 })).json();
  // 4. Your own HTTP client on your machine, for reference.
  const local = await (await fetch(PEET)).json();

  const out = [
    await row('page.goto', nav, 'the browser'),
    await row('fetch() in page.evaluate', inPage, 'the browser'),
    await row('page.request.get', viaRequest, 'Playwright server'),
    await row('fetch() in your script', local, 'your machine'),
  ];
  // JA4's middle part hashes the cipher suites: the same TLS stack keeps it across connections.
  for (const r of out) r.same_tls_stack_as_browser = r.ja4.split('_')[1] === out[0].ja4.split('_')[1];
  console.log(JSON.stringify(out, null, 2));
} finally {
  await browser.close();
}
