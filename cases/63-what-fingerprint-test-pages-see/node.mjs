// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one page load per test page per setup)
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;

const SETUPS = [
  { setup: 'Chromium, headless', engine: 'chromium', family: 'chromium', body: { headless: 'new' } },
  { setup: 'Google Chrome, headful', engine: 'chrome', family: 'chromium', body: { headless: false } },
  { setup: 'Patchright, headful', engine: 'patchright', family: 'chromium', body: { headless: false } },
  { setup: 'Camoufox, defaults', engine: 'camoufox', family: 'firefox', body: { headless: true } },
  { setup: 'WebKit, headless', engine: 'webkit', family: 'webkit', body: { headless: true } },
];
const TYPES = { chromium, firefox, webkit };

// Each test page's own verdict, read from its DOM. The same expressions run in every language;
// "ready" is polled until the page has finished its checks.
const PAGES = [
  {
    key: 'sannysoft', url: 'https://bot.sannysoft.com/',
    ready: "document.querySelectorAll('td.passed, td.failed').length > 10",
    read: "(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()",
  },
  {
    key: 'creepjs', url: 'https://abrahamjuliot.github.io/creepjs/',
    ready: "/\\d+% stealth/.test(document.body.innerText)",
    read: "(() => { const t = document.body.innerText.replace(/\\s+/g, ' '); const m = /(\\d+)% like headless[\\s\\S]*?(\\d+)% headless:[\\s\\S]*?(\\d+)% stealth/.exec(t); return m ? { like_headless_pct: +m[1], headless_pct: +m[2], stealth_pct: +m[3] } : { like_headless_pct: null, headless_pct: null, stealth_pct: null }; })()",
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
async function launch(engine, body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: PROXY, record: false, ...body }),
    });
    if (res.ok) return res.json();
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

const results = [];
for (const s of SETUPS) {
  const session = await launch(s.engine, s.body);
  const browser = await TYPES[s.family].connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  const row = { setup: s.setup, engine: s.engine };
  try {
    const page = await (await browser.newContext()).newPage();
    for (const p of PAGES) {
      try {
        await page.goto(p.url, { timeout: 60000 });
        await page.waitForFunction(p.ready, null, { timeout: 45000 });
        row[p.key] = await page.evaluate(p.read);
      } catch (err) {
        row[p.key] = { error: err.message.split('\n')[0].slice(0, 120) };
      }
    }
  } finally {
    await browser.close();
  }
  results.push(row);
}
console.log(JSON.stringify(results, null, 2));
