// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox } from 'playwright';
import { writeFileSync, readFileSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const STATE_FILE = path.join(tmpdir(), 'cdpfleet-state.json'); // keep it somewhere safe: it holds the login

async function launch(name) {
  const res = await fetch(`https://starter.cdpfleet.com/${name}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
  });
  if (!res.ok) throw new Error(`launch ${name}: ${res.status} ${await res.text()}`);
  return res.json();
}

const cookiesSeen = async (page) => (await (await page.goto('https://httpbin.org/cookies', { timeout: 60000 })).json()).cookies;
const draft = (page) => page.evaluate(() => localStorage.getItem('draft'));

// Session 1 (Chromium): "log in", then save cookies + localStorage to a local file.
const s1 = await launch('chromium');
const b1 = await chromium.connect(s1.wsUrl, { headers: { 'x-api-key': KEY } });
let saved;
try {
  const ctx = await b1.newContext();
  const page = await ctx.newPage();
  await page.goto('https://httpbin.org/cookies/set?session=abc123&user=alice', { timeout: 60000 });
  await page.evaluate(() => localStorage.setItem('draft', 'half-written review'));
  saved = await ctx.storageState();
  writeFileSync(STATE_FILE, JSON.stringify(saved, null, 2));
} finally {
  await b1.close(); // the browser is gone; only state.json remains
}

// Session 2 (Firefox, a fresh browser on whichever server the fleet picks): restore it.
const s2 = await launch('firefox');
const b2 = await firefox.connect(s2.wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const restored = await b2.newContext({ storageState: JSON.parse(readFileSync(STATE_FILE, 'utf8')) });
  const page = await restored.newPage();
  const cookies = await cookiesSeen(page);
  const localStorageValue = await draft(page);

  // The same browser without the state, for comparison.
  const blank = await (await b2.newContext()).newPage();
  const blankCookies = await cookiesSeen(blank);

  console.log(JSON.stringify({
    session_1: { id: s1.sessionId, browser: 'chromium', saved_cookies: saved.cookies.map((c) => `${c.name}@${c.domain}`), saved_origins: saved.origins.map((o) => o.origin) },
    state_file_bytes: statSync(STATE_FILE).size,
    session_2: { id: s2.sessionId, browser: 'firefox', cookies_sent: cookies, local_storage_draft: localStorageValue },
    session_2_without_state: { cookies_sent: blankCookies },
  }, null, 2));
} finally {
  await b2.close();
}
