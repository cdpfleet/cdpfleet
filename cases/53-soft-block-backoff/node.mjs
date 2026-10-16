// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, SOCKS_PROXIES (comma-separated sticky socks5:// URLs: one exit IP each)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXIES = process.env.SOCKS_PROXIES.split(',');
const PAGES = Array.from({ length: 8 }, (_, i) => `https://httpbin.org/anything/catalogue/page-${i + 1}`);

// A stand-in for a rate-limited site, in front of real httpbin requests. Each request really goes
// out through the session's proxy; the limiter decides by the IP httpbin saw (its "origin"):
// 3 pages in a burst, then one page per 5 s (429 + Retry-After: 5), 6 pages per IP in total. An IP
// that comes back before its Retry-After, or is over its quota, gets a challenge page from then on.
function limiter() {
  const ips = new Map();
  return async (route) => {
    const now = Date.now(); // when the request arrived
    const real = await route.fetch({ timeout: 30000 }).catch(() => null);
    if (!real) return route.fulfill({ status: 502, contentType: 'text/plain', body: 'Bad Gateway' }); // the proxy hiccuped
    if (real.status() !== 200) return route.fulfill({ response: real }); // httpbin itself failed
    const ip = (await real.json()).origin;
    const s = ips.get(ip) ?? { served: 0, last: 0, retryAt: 0, challenged: false };
    ips.set(ip, s);
    if (now < s.retryAt || s.served >= 6) s.challenged = true;
    if (s.challenged) {
      return route.fulfill({ status: 403, contentType: 'text/html', body: '<title>Just a moment...</title><p>Checking your browser before accessing the site.</p>' });
    }
    if (s.served >= 3 && now - s.last < 5000) {
      s.retryAt = now + 5000;
      return route.fulfill({ status: 429, headers: { 'retry-after': '5' }, contentType: 'text/plain', body: 'Too Many Requests' });
    }
    s.served++;
    s.last = now;
    return route.fulfill({ response: real });
  };
}

// What the response means: the data, a rate limit (wait), or a block (this IP is done).
function classify(status, text) {
  if (status === 429) return 'rate_limited';
  if (status === 403 || status === 503 || /just a moment|captcha|access denied/i.test(text)) return 'blocked';
  return status === 200 ? 'ok' : 'error';
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return res.json();
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

async function crawl(polite) {
  const { sessionId, wsUrl } = await launch({ proxy: PROXIES[polite ? 1 : 0], headless: 'new', record: false });
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  const site = limiter();
  // Live proxy swap on the session's router: new connections use the new proxy.
  const swapProxy = async (proxy) => {
    const r = await fetch(`https://${new URL(wsUrl).host}/admin/session/proxy`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ session_id: sessionId, proxy }),
    });
    if (!r.ok) throw new Error(`swap ${r.status} ${await r.text()}`);
  };
  const open = async (storageState) => {
    const context = await browser.newContext({ storageState });
    await context.route('https://httpbin.org/anything/**', site);
    return { context, page: await context.newPage() };
  };
  const rows = [];
  let waited = 0;
  let swaps = 0;
  let next = 2;
  let cookieKept = false;
  try {
    let { context, page } = await open();
    // A logged-in state that must survive a proxy swap.
    await page.goto('https://httpbin.org/cookies/set?session=alice-42', { timeout: 60000 });
    for (const [i, url] of PAGES.entries()) {
      const trail = [];
      let row = { page: i + 1, outcome: 'gave up', exit_ip: null };
      for (let attempt = 1; attempt <= 6; attempt++) {
        const res = await page.goto(url, { timeout: 60000 });
        const text = await page.textContent('body');
        const kind = classify(res.status(), text);
        trail.push(res.status());
        if (kind === 'ok') {
          row = { page: i + 1, outcome: 'ok', exit_ip: JSON.parse(text).origin };
          break;
        }
        if (kind === 'rate_limited') {
          // Honour Retry-After; without one, back off exponentially (1, 2, 4… s, capped).
          if (!polite && attempt === 3) break;
          const seconds = polite ? Number(res.headers()['retry-after'] ?? Math.min(2 ** (attempt - 1), 30)) : 0;
          if (seconds) trail.push(`wait ${seconds}s`);
          waited += seconds;
          await new Promise((r) => setTimeout(r, seconds * 1000));
          continue;
        }
        if (kind === 'blocked' && polite && next < PROXIES.length) {
          // This exit is done: move the session to another proxy and continue in a new context
          // (its own connections) that keeps the cookies and storage.
          await swapProxy(PROXIES[next++]);
          const state = await context.storageState();
          await context.close();
          ({ context, page } = await open(state));
          swaps++;
          trail.push('swap proxy');
          continue;
        }
        if (kind === 'error') {
          await new Promise((r) => setTimeout(r, 1000)); // a transient failure: try again
          continue;
        }
        break;
      }
      rows.push({ ...row, trail: trail.join(' → ') });
    }
    await page.goto('https://httpbin.org/cookies', { timeout: 60000 });
    cookieKept = JSON.parse(await page.textContent('body')).cookies.session === 'alice-42';
  } finally {
    await browser.close();
  }
  return {
    client: polite ? 'polite' : 'naive',
    pages_fetched: rows.filter((r) => r.outcome === 'ok').length,
    exits: [...new Set(rows.map((r) => r.exit_ip).filter(Boolean))],
    waited_seconds: waited,
    proxy_swaps: swaps,
    session_cookie_kept: cookieKept,
    pages: rows,
  };
}

// Naive: retries a 429 straight away and has no plan for a block. Polite: waits as told and moves
// to another exit when one is blocked.
const naive = await crawl(false);
const polite = await crawl(true);
console.log(JSON.stringify({ naive, polite }, null, 2));
