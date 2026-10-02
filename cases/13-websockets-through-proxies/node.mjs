// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const ECHO = 'wss://ws.postman-echo.com/raw'; // a public WebSocket echo server
const PROXIES = {
  'residential (any country)': process.env.PROXY_URL,
  'residential (Germany)': process.env.PROXY_URL_DE,
  'datacenter SOCKS5': process.env.SOCKS_PROXIES.split(',')[0],
};

// Runs in the page: connect, then 20 echo round trips, one at a time.
const PROBE = async (url) => {
  const t0 = performance.now();
  const ws = new WebSocket(url);
  await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('WebSocket failed')); });
  const connectMs = performance.now() - t0;
  const rtts = [];
  for (let i = 0; i < 20; i++) {
    const sent = performance.now();
    const echoed = new Promise((ok) => { ws.onmessage = (m) => ok(m.data); });
    ws.send(`ping ${i}`);
    if ((await echoed) !== `ping ${i}`) throw new Error('wrong echo');
    rtts.push(performance.now() - sent);
  }
  ws.close();
  rtts.sort((a, b) => a - b);
  return { connectMs, p50: rtts[10], p95: rtts[18], min: rtts[0] };
};

// One session per proxy. Residential exits drop a connection now and then: one retry
// in a new session, then report the error instead of failing everything.
async function measure(label, proxy) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await measureOnce(label, proxy);
    } catch (err) {
      if (attempt === 2) return { proxy: label, error: err.message.split('\n')[0] };
    }
  }
}

async function measureOnce(label, proxy) {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy, headless: true }),
  });
  if (!res.ok) throw new Error(`launch ${res.status}`);
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    // Where this proxy exits (also gives the page an https origin to open the socket from).
    const geo = await (await page.goto('http://ip-api.com/json/?fields=country,city', { timeout: 30000 })).json();
    await page.goto('https://httpbin.org/html', { timeout: 30000 });
    const r = await page.evaluate(PROBE, ECHO);
    return {
      proxy: label, exit: `${geo.city}, ${geo.country}`,
      connect_ms: Math.round(r.connectMs), rtt_p50_ms: Math.round(r.p50), rtt_p95_ms: Math.round(r.p95), rtt_min_ms: Math.round(r.min),
    };
  } finally {
    await browser.close();
  }
}

const rows = [];
for (const [label, proxy] of Object.entries(PROXIES)) rows.push(await measure(label, proxy));
console.log(JSON.stringify({ echo_server: ECHO, round_trips: 20, results: rows }, null, 2));
