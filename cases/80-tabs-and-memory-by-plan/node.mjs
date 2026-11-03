// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (your plan), PROXY_URL; optional COMMUNITY_API_KEY, CDPFLEET_DEDICATED_API_KEY to compare plans
import { chromium, firefox, webkit } from 'playwright';

const STARTER = 'https://starter.cdpfleet.com';
const API = 'https://cdpfleet.com';
const PAGE = 'https://en.wikipedia.org/wiki/World_War_II'; // a long article: a mid-weight page
const MAX_TABS = 50; // the per-session tab cap on every plan
const PROXY = process.env.PROXY_URL;
const TYPES = { chromium, firefox, webkit };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Each run: an account, an engine and a memory multiplier. Runs whose key isn't set are skipped.
const RUNS = [
  ['COMMUNITY_API_KEY', 'chromium', 1],
  ['CDPFLEET_API_KEY', 'chromium', 1],
  ['CDPFLEET_API_KEY', 'firefox', 1],
  ['CDPFLEET_API_KEY', 'webkit', 1],
  ['CDPFLEET_API_KEY', 'chromium', 2],
  ['CDPFLEET_DEDICATED_API_KEY', 'chromium', 1],
].filter(([env]) => process.env[env]);

const api = (key, path) => fetch(`${API}${path}`, { headers: { 'x-api-key': key } }).then((r) => r.json());

async function run([env, engine, memory]) {
  const key = process.env[env];
  const sub = (await api(key, '/v1/me')).subscription;
  const plan = sub.features?.community ? 'Community' : sub.kind === 'dedicated' ? 'Dedicated' : 'Shared';
  const t0 = Date.now();
  // Community launches use the plan's included proxy; paid plans bring their own.
  const body = { headless: true, ...(memory > 1 ? { memory } : {}), ...(plan === 'Community' ? {} : { proxy: PROXY }) };
  const res = await fetch(`${STARTER}/${engine}/session`, { method: 'POST', headers: { 'x-api-key': key, 'content-type': 'application/json' }, body: JSON.stringify(body) });
  if (!res.ok) return { plan, engine, memory, launch_error: `${res.status} ${await res.text()}` };
  const launch = await res.json();
  const browser = await TYPES[engine].connect(launch.wsUrl, { headers: { 'x-api-key': key } });
  const context = await browser.newContext();
  // Open the article in one new tab after another until the session ends or the tab cap is reached.
  let tabs = 0;
  let clientError = null;
  while (tabs < MAX_TABS) {
    try {
      const page = await context.newPage();
      // A slow load through a shared proxy gets one more try; a closed session doesn't.
      await page.goto(PAGE, { timeout: 60000 }).catch((err) => { if (/Timeout/.test(err.message)) return page.goto(PAGE, { timeout: 60000 }); throw err; });
      tabs++;
    } catch (err) {
      clientError = err.message.split('\n')[0];
      break;
    }
  }
  // At the cap with memory to spare: what does a 51st tab get?
  let tab51 = null;
  if (tabs === MAX_TABS) {
    try { await context.newPage(); tab51 = 'opened'; } catch (err) { tab51 = err.message.split('\n')[0]; }
  }
  await browser.close().catch(() => {});
  // The session's record: why it ended, its weight, peak memory and time.
  let record = null;
  for (let i = 0; i < 20 && !record?.ended_at; i++) {
    await sleep(1500);
    record = (await api(key, '/v1/me/sessions?limit=20')).sessions.find((s) => s.session_id === launch.sessionId);
  }
  return {
    plan, engine, memory, weight: launch.weight, memory_mb: launch.memory_mb, tabs,
    client_error: clientError?.replace(/^.*?(Session closed: .*)$/, '$1') ?? null, tab_51: tab51,
    end_reason: record?.end_reason ?? null, memory_peak_mb: record?.memory_peak_mb ?? null,
    seconds: record?.duration_seconds ?? Math.round((Date.now() - t0) / 1000),
    thread_seconds: record ? record.duration_seconds * record.weight : null,
  };
}

const runs = [];
for (const r of RUNS) runs.push(await run(r)); // one at a time: one site, one session at a time
console.log(JSON.stringify({ language: 'node', page: PAGE, max_tabs: MAX_TABS, measured_at: new Date().toISOString().slice(0, 10), runs }, null, 2));
