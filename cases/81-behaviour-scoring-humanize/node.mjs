// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (one session at a time; public test pages only; nothing is solved or retried to get a result)
import { randomBytes } from 'node:crypto';
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
// One sticky residential exit for the whole run, shared by every setup: a difference between rows is the browser
// and how it was driven, not the IP.
const PROXY = `cdpfleet-resi-country-us-session-c81${randomBytes(3).toString('hex')}-lifetime-30`;
const REPEATS = 3;
// Camoufox's humanize option: false = the cursor jumps straight to the target, true (the default) = a curved path of
// up to 1.5 s, a number = a path of up to that many seconds. Chrome is the reference: plain Playwright clicks.
const SETUPS = [
  { key: 'cf_off', setup: 'Camoufox, humanize: false', engine: 'camoufox', type: firefox, body: { headless: true, humanize: false } },
  { key: 'cf_on', setup: 'Camoufox, humanize: true (default)', engine: 'camoufox', type: firefox, body: { headless: true, humanize: true } },
  { key: 'cf_slow', setup: 'Camoufox, humanize: 3', engine: 'camoufox', type: firefox, body: { headless: true, humanize: 3 } },
  { key: 'chrome', setup: 'Google Chrome, headful', engine: 'chrome', type: chromium, body: { headless: false } },
];
// The same actions in every cell; only the typing speed differs between the two form runs of a session.
const TYPING = { fast: 0, paced: 120 }; // ms between keys
const EMAIL = 'lab81@example.com';
const PASSWORD = 'lab-81!pass'; // the demo form wants 8+ characters with a special character

// deviceandbrowserinfo.com's behavioural bot test: a demo login form that records clicks, keystrokes and timing
// and, on submit, posts them with a fingerprint to its own endpoint, which answers human or bot with its flags.
const FORM = 'https://deviceandbrowserinfo.com/are_you_a_bot_interactions';
// Google's public reCAPTCHA v3 demo prints its server's verify response (with the score); its "Try again" link reloads it.
const RECAPTCHA = 'https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php';

// The same expressions run in every language.
// Count mousemove events on the page, so the table shows what humanize changed.
const COUNT_MOVES = `(() => { window.__lab81 = 0; document.addEventListener('mousemove', () => { window.__lab81++; }, true); return true; })()`;
// The verdict the page shows after submit: isBot and every detail flag that is true.
const VERDICT = `(() => {
  const e = document.getElementById('jsonResult');
  const t = e ? e.innerText : '';
  if (t.length < 10) return null;
  const r = JSON.parse(t);
  const d = r.details || {};
  return { is_bot: r.isBot === true, flags: Object.keys(d).filter((k) => d[k] === true), moves: window.__lab81 };
})()`;
// What the page itself measured about the interaction: the "interactions" part of the body it posted, which is
// embedded as a JSON string and passed to this function.
const MEASURED = `(body) => {
  const i = JSON.parse(body).interactions || {};
  const s = i.emailTimingStats || {};
  return {
    time_to_fill_ms: Math.round(i.timeToFill),
    exact_center_clicks: [i.hasClickedEmailFieldExactCenter, i.hasClickedPasswordFieldExactCenter, i.hasClickedSubmitExactCenter].filter(Boolean).length,
    untrusted_event: i.hasUntrustedEvent === true,
    cdp_mouse_leak: i.cdpMouseLeak === true,
    key_gap_median_ms: typeof s.medianTimeBetweenKeys === 'number' ? Math.round(s.medianTimeBetweenKeys) : null,
  };
}`;
const SCORE = `(() => { if (window.__lab81rc) return null; const m = /"score"\\s*:\\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()`;

async function launch(s) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${s.engine}/session`, {
      method: 'POST', headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: PROXY, record: false, ...s.body }),
    });
    if (res.ok) return s.type.connect((await res.json()).wsUrl, { headers: { 'x-api-key': KEY } });
    if (![429, 503].includes(res.status) || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// A load that fails on the network (proxy timeout) is tried once more; the page's answer is never retried.
async function open(tab, url) {
  for (let attempt = 1; attempt <= 2; attempt++) {
    try {
      await tab.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 });
      return true;
    } catch { /* network failure */ }
  }
  return false;
}

// One form run: click the email field, type, click the password field, type, click Login; read the verdict.
async function form(browser, typing) {
  const out = { typing, verdict: 'error', flags: [], moves: null, action_seconds: null, measured: null };
  const context = await browser.newContext(); // a fresh context: every run is a first visit
  try {
    const tab = await context.newPage();
    let posted = null;
    tab.on('request', (req) => { if (req.url().includes('/fingerprint_bot_test')) posted = req.postData(); });
    if (!(await open(tab, FORM))) return out;
    await sleep(1000);
    await tab.evaluate(COUNT_MOVES);
    const t0 = Date.now();
    await tab.click('#email');
    await tab.keyboard.type(EMAIL, { delay: TYPING[typing] });
    await tab.click('#password');
    await tab.keyboard.type(PASSWORD, { delay: TYPING[typing] });
    await tab.click('#loginForm button[type=submit]');
    out.action_seconds = Math.round((Date.now() - t0) / 100) / 10;
    let v = null;
    for (let i = 0; i < 60 && v === null; i++) { await sleep(500); v = await tab.evaluate(VERDICT).catch(() => null); }
    if (v === null) { out.verdict = 'no verdict'; return out; }
    Object.assign(out, { verdict: v.is_bot ? 'bot' : 'human', flags: v.flags, moves: v.moves });
    if (posted) out.measured = await tab.evaluate(`(${MEASURED})(${JSON.stringify(posted)})`);
    return out;
  } catch {
    return out;
  } finally {
    await context.close().catch(() => {});
  }
}

// Google's v3 demo: wait for the score it asks for on load, move the cursor and click "Try again", read the new score.
async function recaptcha(browser) {
  const out = { before: null, after: null };
  const context = await browser.newContext();
  try {
    const tab = await context.newPage();
    if (!(await open(tab, RECAPTCHA))) return out;
    for (let i = 0; i < 40 && out.before === null; i++) { await sleep(500); out.before = await tab.evaluate(SCORE).catch(() => null); }
    await tab.evaluate('window.__lab81rc = 1'); // marks this document, so the score is read from the reloaded one
    await tab.mouse.move(300, 300);
    await tab.mouse.move(600, 200);
    await tab.click('a[href="/recaptcha-v3-request-scores.php"]');
    for (let i = 0; i < 40 && out.after === null; i++) { await sleep(500); out.after = await tab.evaluate(SCORE).catch(() => null); }
    return out;
  } catch {
    return out;
  } finally {
    await context.close().catch(() => {});
  }
}

const runs = [];
const scores = [];
// Serial: one session at a time; the repeats are interleaved so a slow minute doesn't land on one setup.
for (let repeat = 1; repeat <= REPEATS; repeat++) {
  for (const s of SETUPS) {
    const base = { setup: s.setup, key: s.key, repeat };
    let browser;
    try {
      browser = await launch(s);
    } catch {
      for (const typing of Object.keys(TYPING)) runs.push({ ...base, typing, verdict: 'error', flags: [], moves: null, action_seconds: null, measured: null });
      scores.push({ ...base, before: null, after: null });
      continue;
    }
    for (const typing of Object.keys(TYPING)) runs.push({ ...base, ...(await form(browser, typing)) });
    scores.push({ ...base, ...(await recaptcha(browser)) });
    await browser.close().catch(() => {});
  }
}
console.log(JSON.stringify({ measured_at: new Date().toISOString().slice(0, 10), runs, recaptcha: scores }, null, 2));
