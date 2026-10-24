// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (sticky US exits from the cdpfleet residential proxy, Tier 1; one load per bot-protection page)
import { randomBytes } from 'node:crypto';
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = randomBytes(3).toString('hex');
// One sticky exit per setup, used with and without the tunnel: the only difference between the two rows is vpn_tunnel.
const exitFor = (id) => `cdpfleet-resi-country-us-session-t${id}${RUN}-lifetime-30`;
const SETUPS = {
  chrome: { id: 'c', setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, headless: false },
  camoufox: { id: 'f', setup: 'Camoufox, defaults', engine: 'camoufox', type: firefox, headless: true },
};
const NETWORKS = {
  proxy: { label: 'residential proxy', body: {} },
  tunnel: { label: 'residential proxy + vpn_tunnel', body: { vpn_tunnel: true } },
};

// The WebRTC leak test from case 54: ICE candidates against a public STUN server. A "srflx" candidate is the public IP
// the STUN server saw. The same expression runs in every language.
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
// The protocol of the page's own request: h2 over TCP, or h3 over QUIC (UDP).
const PROTOCOL = `performance.getEntriesByType('navigation')[0]?.nextHopProtocol || null`;
// Cloudflare's interstitial or block page, or the real page.
const CF_STATE = `(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\\s+/g, ' ');
  return /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
})()`;
// DataDome's iframe from captcha-delivery.com: a device check, a CAPTCHA, or a block (t=bv) — as in case 65.
const DD_STATE = `(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\\/captcha\\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()`;

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(s, network) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${s.engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: exitFor(s.id), headless: s.headless, record: false, ...NETWORKS[network].body }),
    });
    if (res.ok) return s.type.connect((await res.json()).wsUrl, { headers: { 'x-api-key': KEY } });
    if (![429, 503].includes(res.status) || attempt === 6) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Residential peers drop a connection now and then: one retry.
async function go(tab, url) {
  try {
    return await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });
  } catch {
    return tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 45000 });
  }
}

// One load, then watch the page once a second for 20 s (Cloudflare) or 12 s (DataDome) without touching it.
async function watch(context, url, kind) {
  const tab = await context.newPage();
  const t0 = Date.now();
  try {
    try {
      await go(tab, url);
    } catch {
      return { outcome: 'error', seconds: null };
    }
    let seen = false;
    const until = Date.now() + (kind === 'cf' ? 20000 : 12000);
    while (Date.now() < until) {
      const s = await tab.evaluate(kind === 'cf' ? CF_STATE : DD_STATE).catch(() => null);
      if (s === 'blocked') return { outcome: 'blocked', seconds: null };
      if (s === 'challenge' && kind === 'dd') return { outcome: 'challenge', seconds: null };
      if (s === 'challenge' || s === 'checking') seen = true;
      if (s === 'page') return seen ? { outcome: 'cleared', seconds: Math.round((Date.now() - t0) / 1000) } : { outcome: 'content', seconds: null };
      await sleep(1000);
    }
    return { outcome: seen ? 'challenge' : 'error', seconds: null };
  } finally {
    await tab.close();
  }
}

async function row(setupKey, network) {
  const s = SETUPS[setupKey];
  const browser = await launch(s, network);
  const out = { setup: s.setup, network: NETWORKS[network].label };
  try {
    const context = await browser.newContext();
    const tab = await context.newPage();
    await go(tab, 'https://api.ipify.org/?format=json');
    out.exit_ip = JSON.parse(await tab.textContent('body')).ip;
    const { api, candidates } = await tab.evaluate(`(${GATHER})()`);
    // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
    const parsed = candidates.map((c) => { const f = c.split(' '); return { address: f[4], type: f[7] }; });
    out.webrtc_api = api;
    out.webrtc_ips = [...new Set(parsed.filter((c) => /^\d+\.\d+\.\d+\.\d+$/.test(c.address) && c.address !== '0.0.0.0').map((c) => c.address))];
    out.mdns_only = out.webrtc_ips.length === 0 && parsed.some((c) => c.address.endsWith('.local'));
    await tab.close();
    // HTTP/3: a page announces it over HTTP/2 (Alt-Svc); the browser can switch to QUIC on a later request. Four loads
    // in fresh tabs, a pause between them.
    out.protocols = [];
    for (let i = 0; i < 4; i++) {
      const t = await context.newPage();
      await go(t, 'https://cloudflare-quic.com/').catch(() => {});
      out.protocols.push(await t.evaluate(PROTOCOL).catch(() => null));
      await t.close();
      await sleep(2500);
    }
    await context.close();
    // The bot-protection pages, each in a fresh context: Cloudflare's challenge test page (case 64) and DataDome's
    // own site (case 65).
    for (const [key, url, kind] of [['scrapingcourse', 'https://www.scrapingcourse.com/cloudflare-challenge', 'cf'], ['datadome', 'https://datadome.co/', 'dd']]) {
      const c = await browser.newContext();
      out[key] = await watch(c, url, kind);
      await c.close();
    }
  } finally {
    await browser.close();
  }
  return out;
}

const rows = [];
for (const setupKey of Object.keys(SETUPS)) {
  for (const network of Object.keys(NETWORKS)) rows.push(await row(setupKey, network));
}
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), rows }, null, 2));
