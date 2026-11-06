// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL; // the same proxy for the Function calls and the sessions
const API = 'https://cdpfleet.com';
const STARTER = 'https://starter.cdpfleet.com';
const PAGES = [
  ['example.com', 'https://example.com/'],
  ['books.toscrape.com', 'https://books.toscrape.com/'],
  ['wikipedia', 'https://en.wikipedia.org/wiki/Web_scraping'],
];
const TASKS = ['screenshot', 'pdf', 'markdown'];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const since = new Date(Date.now() - 60000).toISOString();

// The record of a session or a Function call in the account's history (a call's x-call-id is its
// session id there). Billed thread-seconds = (ended_at - started_at) × weight.
async function billed(id) {
  for (let i = 0; i < 40; i++) {
    const r = await fetch(`${API}/v1/me/sessions?from=${encodeURIComponent(since)}&limit=200`, { headers: { 'x-api-key': KEY } });
    const s = (await r.json()).sessions?.find((x) => x.session_id === id);
    if (s?.ended_at) return { engine: s.engine, weight: s.weight, thread_seconds: Math.round((Date.parse(s.ended_at) - Date.parse(s.started_at)) * s.weight / 100) / 10 };
    await sleep(2000);
  }
  return null;
}

// What a file is: PNG width × height; for a PDF, whether it has fonts (a text PDF), its page objects
// (0 when they sit in compressed object streams) and the tallest image in it.
function describe(task, buf) {
  if (task === 'screenshot') return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
  if (task === 'pdf') {
    const s = buf.toString('latin1');
    const heights = [...s.matchAll(/\/Height (\d+)/g)].map((m) => Number(m[1]));
    return { text_pdf: /\/Font\b/.test(s), pages: (s.match(/\/Type\s*\/Page(?![s\w])/g) || []).length, image_height: heights.length ? Math.max(...heights) : 0 };
  }
  return {};
}

// (a) The Function: one POST, the file in the response. One retry if the call fails.
async function viaFunction(task, url) {
  const body = { url, proxy: PROXY, ...(task === 'screenshot' ? { full_page: true } : task === 'pdf' ? { format: 'A4' } : {}) };
  const t = Date.now();
  const calls = [];
  let res, buf;
  for (let attempt = 1; attempt <= 2; attempt++) {
    res = await fetch(`${API}/v1/functions/${task}`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(40000),
    });
    buf = Buffer.from(await res.arrayBuffer());
    if (res.headers.get('x-call-id')) calls.push(res.headers.get('x-call-id'));
    if (res.status === 200) break;
  }
  const ms = Date.now() - t;
  const records = [];
  for (const id of calls) records.push(await billed(id));
  const out = {
    status: res.status, attempts: calls.length, ms, bytes: buf.length,
    content_type: res.headers.get('content-type'),
    partial: res.headers.get('x-cdpfleet-partial'),
    engine: records.at(-1)?.engine ?? null,
    weight: records.at(-1)?.weight ?? null,
    thread_seconds: records.every(Boolean) ? Math.round(records.reduce((a, r) => a + r.thread_seconds, 0) * 10) / 10 : null,
    ...(res.status === 200 ? describe(task, buf) : {}),
  };
  if (task === 'markdown' && res.status === 200) {
    const md = buf.toString('utf8');
    out.first_line = md.split('\n')[0].slice(0, 80);
    out.has_source_line = /\nSource: /.test(md);
  }
  return out;
}

// (b) Your own session: launch, connect, open the page, capture, close.
async function launch() {
  const res = await fetch(`${STARTER}/chromium/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: PROXY, headless: true }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const { sessionId, wsUrl } = await res.json();
  return { sessionId, browser: await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } }) };
}
async function open(browser, url) {
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  // A proxy drops the odd connection: one retry.
  await page.goto(url, { timeout: 45000 }).catch(() => page.goto(url, { timeout: 45000 }));
  return page;
}
// Markdown has no Playwright call: you'd add a converter (turndown, markdownify, …). Measured here:
// the HTML you would convert and the page's innerText.
async function capture(page, task) {
  if (task === 'screenshot') { const buf = await page.screenshot({ fullPage: true }); return { bytes: buf.length, ...describe(task, buf) }; }
  if (task === 'pdf') { const buf = await page.pdf({ format: 'A4' }); return { bytes: buf.length, ...describe(task, buf) }; }
  const html = Buffer.byteLength(await page.content());
  const text = Buffer.byteLength(await page.locator('body').innerText());
  return { bytes: html, html_bytes: html, text_bytes: text };
}
async function viaSession(task, url) {
  const t = Date.now();
  const { sessionId, browser } = await launch();
  const launchMs = Date.now() - t;
  let out;
  try {
    out = await capture(await open(browser, url), task);
  } finally {
    await browser.close(); // closing the connection ends the session
  }
  const ms = Date.now() - t;
  const rec = await billed(sessionId);
  return { ms, launch_ms: launchMs, engine: rec?.engine ?? null, weight: rec?.weight ?? null, thread_seconds: rec?.thread_seconds ?? null, ...out };
}

const runs = [];
for (const [name, url] of PAGES) {
  for (const task of TASKS) {
    runs.push({ page: name, task, function: await viaFunction(task, url), session: await viaSession(task, url) });
  }
}

// (c) One session for all nine captures: launch once, three pages, three outputs each.
const t = Date.now();
const { sessionId, browser } = await launch();
let captures = 0;
try {
  for (const [, url] of PAGES) {
    const page = await open(browser, url);
    for (const task of TASKS) { await capture(page, task); captures++; }
    await page.close();
  }
} finally {
  await browser.close();
}
const oneMs = Date.now() - t;
const one = await billed(sessionId);

console.log(JSON.stringify({
  language: 'node',
  proxy: 'the same PROXY_URL for every Function call and every session',
  runs,
  one_session: { captures, ms: oneMs, thread_seconds: one?.thread_seconds ?? null, weight: one?.weight ?? null },
  measured_at: new Date().toISOString().slice(0, 10),
}, null, 2));
