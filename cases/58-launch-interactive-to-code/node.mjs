// npm install playwright@1.60.0 ws
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import { chromium } from 'playwright';
import WebSocket from 'ws';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

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
    await sleep((Number(res.headers.get('retry-after')) || 5) * 1000);
  }
}

// 1. Launch with a start page and no client: the browser opens it by itself. A twin session
//    with the same options gets no viewer at all, to compare with.
const body = {
  proxy: PROXY, headless: true, live_view: true, record: false,
  start_url: 'https://httpbin.org/', inactivity_timeout: '30s',
};
const session = await launch('chromium', body);
const twin = await launch('chromium', body);

// 2. A live link in control mode — what "Launch interactive" in the dashboard opens for you.
const res = await fetch(`https://${new URL(session.wsUrl).host}/admin/session/live`, {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ session_id: session.sessionId, mode: 'control' }),
});
const live = await res.json();

// The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
const viewer = new WebSocket(live.live_url);
const seen = [];
let frames = 0;
viewer.on('message', (data, binary) => {
  if (binary) { frames++; return; }
  const m = JSON.parse(data);
  if (m.type === 'pages') {
    const url = m.pages.find((p) => p.targetId === m.current)?.url;
    if (url && seen.at(-1) !== url) seen.push(url);
  }
});
await new Promise((ok, fail) => { viewer.once('open', ok); viewer.once('error', fail); });
const send = (m) => viewer.send(JSON.stringify(m));
const waitForUrl = async (part) => { for (let i = 0; i < 60 && !seen.some((u) => u.includes(part)); i++) await sleep(500); return seen.some((u) => u.includes(part)); };
const key = (k) => { send({ type: 'key', action: 'keyDown', key: k, code: k }); send({ type: 'key', action: 'keyUp', key: k, code: k }); };

// 3. The person (here a scripted stand-in): goes to the form from the address bar, then fills
//    the first three fields from the keyboard — Tab to a field, type.
const startShown = await waitForUrl('httpbin.org');
send({ type: 'navigate', url: 'https://httpbin.org/forms/post' });
const formShown = await waitForUrl('/forms/post');
await sleep(1500);
for (const text of ['Ada Lovelace', '+44 20 7946 0000', 'ada@example.com']) {
  key('Tab');
  await sleep(200);
  send({ type: 'text', text });
  await sleep(300);
}
// 4. Then only watches — no input for 45 s, past the 30 s inactivity_timeout.
await sleep(45000);

// The unwatched twin has had no input and no viewer for as long: it should be gone.
let twinAlive = true;
try {
  await (await chromium.connect(twin.wsUrl, { headers: { 'x-api-key': KEY }, timeout: 15000 })).close();
} catch {
  twinAlive = false;
}

// 5. Your code joins the same browser and finds the person's page in the default context.
const browser = await chromium.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
const out = {
  live_protocol: live.protocol, start_page_shown: startShown, form_shown: formShown, frames_while_by_hand: frames,
  watched_session_alive_after_45s: browser.isConnected(), unwatched_twin_alive_after_45s: twinAlive,
};
try {
  const pages = browser.contexts()[0].pages();
  const page = pages.find((p) => p.url().includes('/forms/post'));
  out.pages_found = pages.map((p) => p.url());
  out.typed_by_hand = {
    custname: await page.inputValue('input[name=custname]'),
    custtel: await page.inputValue('input[name=custtel]'),
    custemail: await page.inputValue('input[name=custemail]'),
  };
  // 6. The code finishes the job: the choices a script makes reliably, then submit.
  await page.check('input[name=size][value=medium]');
  await page.check('input[name=topping][value=cheese]');
  await page.fill('input[name=delivery]', '19:30');
  await Promise.all([page.waitForURL('**/post'), page.click('form button')]);
  const echo = JSON.parse(await page.textContent('body'));
  out.submitted = echo.form;
  await sleep(1500);
  out.viewer_saw = seen;
} finally {
  viewer.close();
  await browser.close();
}
// 7. The account API's session history: how long each session ran and why it ended.
async function history(sessionId) {
  for (let i = 0; i < 30; i++) {
    const res = await fetch(`https://cdpfleet.com/v1/me/sessions?limit=50&from=${new Date(Date.now() - 3600000).toISOString()}`, { headers: { 'x-api-key': KEY } });
    const row = (await res.json()).sessions.find((s) => s.session_id === sessionId);
    if (row?.ended_at) return { seconds: Math.round((Date.parse(row.ended_at) - Date.parse(row.started_at)) / 1000), end_reason: row.end_reason };
    await sleep(2000);
  }
  return null;
}
out.watched_session = await history(session.sessionId);
out.unwatched_twin = await history(twin.sessionId);
console.log(JSON.stringify(out, null, 2));
