// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (each setup gets its own sticky US exit from the cdpfleet residential proxy; a few KB of traffic)
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
// A sticky residential exit per setup: one IP for the whole session, to compare with.
const RUN = Math.random().toString(36).slice(2, 8);
const stickyExit = (i) => `cdpfleet-resi-country-us-session-w54${RUN}${i}-lifetime-10`;

// What a WebRTC leak test runs: gather ICE candidates against a public STUN server. A "srflx"
// candidate carries the public IP the STUN server saw; UDP doesn't go through an HTTP/SOCKS proxy —
// on cdpfleet, proxied sessions can't send UDP at all, so only an anonymous mDNS host candidate is left.
const GATHER = `async () => {
  if (typeof RTCPeerConnection === 'undefined') return { api: false, candidates: [] };
  const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
  const candidates = [];
  pc.onicecandidate = (e) => { if (e.candidate && e.candidate.candidate) candidates.push(e.candidate.candidate); };
  pc.createDataChannel('probe');
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise((done) => {
    const timer = setTimeout(done, 8000);
    pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === 'complete') { clearTimeout(timer); done(); } };
  });
  pc.close();
  return { api: true, candidates };
}`;

const SETUPS = [
  { setup: 'Chromium', engine: 'chromium', body: { headless: 'new' } },
  { setup: 'Google Chrome', engine: 'chrome', body: { headless: 'new' } },
  { setup: 'Firefox', engine: 'firefox', body: { headless: true } },
  { setup: 'Camoufox (defaults: geoip on)', engine: 'camoufox', body: { headless: true } },
  { setup: 'Camoufox, block_webrtc: true', engine: 'camoufox', body: { headless: true, block_webrtc: true } },
];
const FIREFOX_FAMILY = ['firefox', 'camoufox'];

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(engine, body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return res.json();
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// Residential peers drop a connection now and then: up to two retries.
async function go(page, url) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await page.goto(url, { timeout: 60000 });
    } catch (err) {
      if (attempt === 3) throw err;
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

const results = [];
for (const [i, s] of SETUPS.entries()) {
  const session = await launch(s.engine, { ...s.body, proxy: stickyExit(i), record: false });
  const browser = await (FIREFOX_FAMILY.includes(s.engine) ? firefox : chromium).connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    await go(page, 'https://api.ipify.org/?format=json');
    const exitIp = JSON.parse(await page.textContent('body')).ip;
    const { api, candidates } = await page.evaluate(`(${GATHER})()`);
    // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
    const parsed = candidates.map((c) => { const f = c.split(' '); return { address: f[4], type: f[7] }; });
    const publicIps = [...new Set(parsed.filter((c) => /^\d+\.\d+\.\d+\.\d+$/.test(c.address) && c.address !== '0.0.0.0').map((c) => c.address))];
    results.push({
      setup: s.setup,
      exit_ip: exitIp,
      webrtc_api: api,
      candidates: candidates.length,
      mdns_host_candidates: parsed.filter((c) => c.type === 'host' && c.address.endsWith('.local')).length,
      public_ips: publicIps,
      shows_exit_ip: publicIps.includes(exitIp),
      leaks_other_ip: publicIps.some((ip) => ip !== exitIp),
    });
  } finally {
    await browser.close();
  }
}
console.log(JSON.stringify(results, null, 2));
