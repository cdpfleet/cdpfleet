// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one load per public test page per setup)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;

const SETUPS = [
  { setup: 'Brave, headless', engine: 'brave', headless: true },
  { setup: 'Brave, headful', engine: 'brave', headless: false },
  { setup: 'Google Chrome, headful', engine: 'chrome', headless: false },
];

// Two made-up sites on different registrable domains, served by page routing: the browser
// treats them as two first-party sites, and no request leaves the browser.
const SITES = { A: 'https://site-a.example/', B: 'https://site-b.test/' };
const HTML = '<!doctype html><title>lab 83</title><p>A page with nothing on it but this probe.</p>';

// What a fingerprinting script reads, the same in every language: a canvas hash (drawn twice),
// an OfflineAudioContext hash, cores, memory, the plugin list, the screen and the window size.
const PROBE = `(async () => {
  const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
  const draw = () => {
    const c = document.createElement('canvas'); c.width = 240; c.height = 60;
    const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
    x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
    x.fillStyle = '#069'; x.fillText('cdpfleet fp 83 <@>', 2, 15);
    x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 83 <@>', 4, 17);
    return c.toDataURL();
  };
  const audio = async () => {
    try {
      const ctx = new OfflineAudioContext(1, 44100, 44100);
      const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
      const k = ctx.createDynamicsCompressor();
      k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
      o.connect(k); k.connect(ctx.destination); o.start(0);
      const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
      let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
      return fnv(s);
    } catch (e) { return 'error: ' + e.name; }
  };
  const c1 = draw(), c2 = draw();
  const plugins = Array.from(navigator.plugins).map((p) => p.name);
  return {
    site: location.hostname,
    canvas: fnv(c1), canvas_same_in_page: c1 === c2,
    audio: await audio(),
    cores: navigator.hardwareConcurrency, memory: navigator.deviceMemory ?? null,
    plugins: fnv(plugins.join(',')), plugin_names: plugins,
    screen: screen.width + 'x' + screen.height, window: outerWidth + 'x' + outerHeight,
  };
})()`;

// Each public test page's own verdict, read from its DOM; "ready" is polled until the page has
// finished its checks. The same expressions run in every language.
const PAGES = [
  {
    key: 'creepjs', url: 'https://abrahamjuliot.github.io/creepjs/',
    ready: "/\\d+% stealth/.test(document.body.innerText) && /privacy: \\S/.test(document.body.innerText)",
    // A section CreepJS caught lying gets its hash marked with the "lies" class.
    read: "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const m = /(\\d+)% like headless[\\s\\S]*?(\\d+)% headless:[\\s\\S]*?(\\d+)% stealth/.exec(t); const privacy = /privacy: (\\S+)/.exec(t); const noise = /rendering: (\\d+% \\w+ noise)/.exec(t); return { like_headless_pct: m ? +m[1] : null, headless_pct: m ? +m[2] : null, stealth_pct: m ? +m[3] : null, privacy: privacy ? privacy[1] : null, canvas_noise: noise ? noise[1] : null, lies: Array.from(document.querySelectorAll('.lies')).map((e) => (e.previousElementSibling ? e.previousElementSibling.innerText.trim() : '?')) }; })()",
  },
  {
    key: 'browserleaks', url: 'https://browserleaks.com/canvas',
    ready: "/Uniqueness\\s+\\d/.test(document.body.innerText)",
    read: "(() => { const t = document.body.innerText; const sig = /Signature\\s+([0-9A-Fa-f]{32})/.exec(t); const u = /Uniqueness\\s+([^\\n]+)/.exec(t); return { signature: sig ? sig[1] : null, uniqueness: u ? u[1].trim() : null }; })()",
  },
  {
    key: 'sannysoft', url: 'https://bot.sannysoft.com/',
    ready: "document.querySelectorAll('td.passed, td.failed').length > 10",
    read: "(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()",
  },
  {
    key: 'deviceandbrowserinfo', url: 'https://deviceandbrowserinfo.com/are_you_a_bot',
    ready: "document.body.innerText.indexOf('\"isBot\"') >= 0",
    read: "(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('\"isBot\"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()",
  },
  {
    key: 'fingerprint', url: 'https://demo.fingerprint.com/playground',
    ready: "/Suspect Score \\d+/.test(document.body.innerText.replace(/\\s+/g, ' '))",
    // Only the bot verdict and the suspect score are read (the page also names proxy networks).
    read: "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const bot = /\\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\\x20-\\x7e]/g, '').replace(/\\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()",
  },
];

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(engine, headless) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: PROXY, record: false, headless }),
    });
    if (res.ok) return res.json();
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

async function siteContext(browser) {
  const context = await browser.newContext();
  for (const url of Object.values(SITES)) await context.route(`${url}**`, (route) => route.fulfill({ contentType: 'text/html', body: HTML }));
  return context;
}

// One visit = a new tab in the given context, the probe, the tab closed.
async function visit(context, label, launchNo, contextNo, site) {
  const page = await context.newPage();
  try {
    await page.goto(SITES[site]);
    return { visit: label, launch: launchNo, context: contextNo, ...(await page.evaluate(PROBE)) };
  } finally {
    await page.close();
  }
}

const out = { setups: [] };
for (const s of SETUPS) {
  const row = { setup: s.setup, engine: s.engine, headless: s.headless, visits: [], pages: {} };
  // Launch 1: site A, site B, site A again in a new tab, then site A in a second context.
  let session = await launch(s.engine, s.headless);
  let browser = await chromium.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const one = await siteContext(browser);
    row.visits.push(await visit(one, 'site A', 1, 1, 'A'));
    row.visits.push(await visit(one, 'site B', 1, 1, 'B'));
    row.visits.push(await visit(one, 'site A again', 1, 1, 'A'));
    const two = await siteContext(browser);
    row.visits.push(await visit(two, 'site A, second context', 1, 2, 'A'));
  } finally {
    await browser.close();
  }
  // Launch 2: sites A and B again, then each public test page once, in a fresh context.
  session = await launch(s.engine, s.headless);
  browser = await chromium.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const three = await siteContext(browser);
    row.visits.push(await visit(three, 'site A, next launch', 2, 1, 'A'));
    row.visits.push(await visit(three, 'site B, next launch', 2, 1, 'B'));
    const page = await (await browser.newContext()).newPage();
    for (const p of PAGES) {
      try {
        // A proxy tunnel drop is tried once more; a page that loaded is never reloaded.
        try { await page.goto(p.url, { timeout: 60000 }); } catch { await page.goto(p.url, { timeout: 60000 }); }
        await page.waitForFunction(p.ready, null, { timeout: 45000 });
        row.pages[p.key] = await page.evaluate(p.read);
      } catch (err) {
        row.pages[p.key] = { error: err.message.split('\n')[0].slice(0, 120) };
      }
    }
  } finally {
    await browser.close();
  }
  out.setups.push(row);
}
console.log(JSON.stringify(out, null, 2));
