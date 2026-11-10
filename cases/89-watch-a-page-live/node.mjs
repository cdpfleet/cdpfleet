// Node.js 18+, no packages. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import crypto from 'node:crypto';

const KEY = process.env.CDPFLEET_API_KEY;
const API = 'https://cdpfleet.com';
// A public page that changes on its own: it asks its server for the ISS position about once a second
// (XHR to /w/ajax/realtime) and rewrites a small table with it.
const PAGE = 'https://wheretheiss.at/';
const WATCH = {
  url: PAGE,
  browser: 'chrome',
  duration: 300, // a safety net: the program stops the job itself
  reload_every: 15,
  filter: { url: ['*/w/ajax/realtime*'], selector: '.span3 table.table-striped', types: ['xhr', 'fetch', 'dom'] },
};
const STOP_AFTER_S = 40; // DELETE the job this long after starting it
const ENDED = ['done', 'failed', 'stopped', 'interrupted'];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const secs = (ms) => Math.round(ms / 100) / 10;
const round = (x, d = 1) => Math.round(x * 10 ** d) / 10 ** d;

async function api(method, path, body) {
  const res = await fetch(`${API}${path}`, {
    method,
    headers: { 'x-api-key': KEY, ...(body ? { 'content-type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch { /* not JSON */ }
  return { status: res.status, json, text };
}

// x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>, checked on the raw bytes.
function verify(secret, header, rawBody) {
  const { t, v1 } = Object.fromEntries((header || '').split(',').map((kv) => kv.split('=')));
  if (!t || !v1) return false;
  const expected = crypto.createHmac('sha256', secret).update(`${t}.${rawBody}`).digest();
  const given = Buffer.from(v1, 'hex');
  return given.length === expected.length && crypto.timingSafeEqual(given, expected);
}

// Great-circle distance in km between two {lat, lon}.
function km(a, b) {
  const rad = (d) => (d * Math.PI) / 180;
  const h = Math.sin(rad(b.lat - a.lat) / 2) ** 2 + Math.cos(rad(a.lat)) * Math.cos(rad(b.lat)) * Math.sin(rad(b.lon - a.lon) / 2) ** 2;
  return 2 * 6371 * Math.asin(Math.sqrt(h));
}

// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour (raw body and
// headers) and reads them back with GET. Use your own public https endpoint.
const hook = `${API}/e2e/hook/${crypto.randomBytes(18).toString('base64url')}`;
const secret = (await api('GET', '/v1/me/webhook-secret')).json.secret;

// 1. Start the watch: XHR/fetch calls matching the glob, and changes under the selector, with a webhook.
const t0 = Date.now();
const start = await api('POST', '/v1/functions/watch', { ...WATCH, deliver: { webhook: { url: hook, events: ['event', 'done'] } } });
if (start.status !== 202) throw new Error(`watch ${start.status} ${start.text}`);
const id = start.json.job_id;

// 2. Follow it live on the SSE stream; stop it with DELETE after STOP_AFTER_S seconds.
let stop = null;
const stopTimer = setTimeout(async () => {
  const t = Date.now();
  const r = await api('DELETE', `/v1/jobs/${id}`);
  stop = { at: t, status: r.status, reply: r.json?.status ?? null };
}, STOP_AFTER_S * 1000);

const live = []; // every event as it arrived: {ev, received}
const seen = { progress: 0, part: 0, event: 0, done: 0 };
let doneData = null;
const sse = await fetch(`${API}/v1/jobs/${id}/events`, { headers: { 'x-api-key': KEY, accept: 'text/event-stream' }, signal: AbortSignal.timeout(240000) });
const decoder = new TextDecoder();
let buf = '';
read: for await (const chunk of sse.body) {
  buf += decoder.decode(chunk, { stream: true });
  for (let i; (i = buf.indexOf('\n\n')) >= 0;) {
    const block = buf.slice(0, i);
    buf = buf.slice(i + 2);
    const name = /^event: ?(.*)$/m.exec(block)?.[1];
    const data = block.split('\n').filter((l) => l.startsWith('data:')).map((l) => l.slice(5).trim()).join('\n');
    if (!name) continue; // a keep-alive comment
    seen[name] = (seen[name] || 0) + 1;
    if (name === 'event') for (const ev of JSON.parse(data).events) live.push({ ev, received: Date.now() });
    if (name === 'done') { doneData = JSON.parse(data); break read; }
  }
}
clearTimeout(stopTimer);
const tEnd = Date.now();

// 3. The job as it ended, and its events as stored.
let job = (await api('GET', `/v1/jobs/${id}`)).json;
for (let i = 0; i < 20 && !ENDED.includes(job.status); i++) { await sleep(2000); job = (await api('GET', `/v1/jobs/${id}`)).json; }
const stored = [];
for (let cursor = 0; cursor !== null;) {
  const r = (await api('GET', `/v1/jobs/${id}/results?cursor=${cursor}`)).json;
  stored.push(...r.results);
  if (r.next_cursor === cursor) break;
  cursor = r.next_cursor;
}

// 4. The webhook deliveries: job.event batches (at most one per 2 s) and job.done. Wait for job.done.
let items = [];
for (const end = Date.now() + 90000; Date.now() < end; await sleep(2000)) {
  items = (await (await fetch(hook)).json()).items;
  if (items.some((d) => d.event === 'job.done')) break;
}
const hooks = items.map((d) => ({ ...d, json: JSON.parse(d.body) }));
const batches = hooks.filter((d) => d.event === 'job.event');
const doneHook = hooks.find((d) => d.event === 'job.done');
const hookEvents = batches.flatMap((d) => d.json.data?.events || []);
const sent = batches.map((d) => Date.parse(d.json.created_at)).sort((a, b) => a - b);
const gaps = sent.slice(1).map((t, i) => t - sent[i]);
// How long after its newest event each batch was signed (the signature's t, whole seconds).
const delays = batches.map((d) => Number(/t=(\d+)/.exec(d.signature || '')?.[1]) - Math.max(...d.json.data.events.map((e) => Date.parse(e.at))) / 1000).sort((a, b) => a - b);
const key = (e) => `${e.type} ${e.at} ${e.url || e.selector}`;
const storedKeys = new Set(stored.map(key));
const hookKeys = new Set(hookEvents.map(key));

// 5. What the events say: the ISS position from each XHR response, the table text from each DOM change.
// An XHR still in flight when the page reloads is reported without a response body.
const positions = stored.filter((e) => e.type === 'xhr' && e.response_body).map((e) => {
  const d = JSON.parse(e.response_body).data;
  return { at: Date.parse(e.at), lat: d.lat, lon: d.lon, time: d.time };
});
const first = positions[0];
const last = positions.at(-1);
const lags = live.map((x) => (x.received - Date.parse(x.ev.at)) / 1000).sort((a, b) => a - b);
const byType = (list) => Object.fromEntries(['xhr', 'fetch', 'ws', 'dom'].map((t) => [t, list.filter((e) => e.type === t).length]).filter(([, n]) => n));
const firstOf = (t) => { const x = live.find((e) => e.ev.type === t); return x ? secs(x.received - t0) : null; };
const dom = stored.filter((e) => e.type === 'dom');

// 6. The bill: a watch runs in your own browser session, one thread for a headless Chrome.
const sessions = (await api('GET', `/v1/me/sessions?from=${encodeURIComponent(new Date(t0 - 60000).toISOString())}&limit=200`)).json?.sessions || [];
const session = sessions.find((s) => s.session_id === job.session_id || s.id === job.session_id);

console.log(JSON.stringify({
  language: 'node',
  page: PAGE,
  watch: { ...WATCH, stop_after_s: STOP_AFTER_S, start_status: start.status },
  sse: {
    events: live.length, by_type: byType(live.map((x) => x.ev)), messages: seen,
    first_event_seconds: live.length ? secs(live[0].received - t0) : null,
    first_xhr_seconds: firstOf('xhr'), first_dom_seconds: firstOf('dom'),
    lag_seconds: lags.length ? { min: round(lags[0], 2), median: round(lags[Math.floor(lags.length / 2)], 2), max: round(lags.at(-1), 2) } : null,
    done_status: doneData?.status ?? null, stream_seconds: secs(tEnd - t0),
  },
  stop: stop && { after_seconds: secs(stop.at - t0), status: stop.status, reply: stop.reply, seconds_to_ended: secs(Date.parse(job.ended_at) - stop.at) },
  job: {
    status: job.status, own_session: job.own_session, concurrency: job.concurrency, runs: job.runs, progress: job.progress,
    billed_seconds: job.billed_seconds, run_seconds: secs(Date.parse(job.ended_at) - Date.parse(job.started_at)),
    results: job.results ? { parts: job.results.parts, bytes: job.results.bytes } : null, error: job.error,
  },
  session: session ? { engine: session.engine, weight: session.weight, seconds: secs(Date.parse(session.ended_at) - Date.parse(session.started_at)) } : null,
  stored: {
    events: stored.length, by_type: byType(stored), same_as_live: stored.length === live.length && live.every((x) => storedKeys.has(key(x.ev))),
    xhr_urls_match_glob: stored.filter((e) => e.type === 'xhr').every((e) => e.url.includes('/w/ajax/realtime') && e.status === 200 && e.method === 'GET'),
    xhr_without_body: stored.filter((e) => e.type === 'xhr' && !e.response_body).length,
    positions: positions.length, distinct_positions: new Set(positions.map((p) => `${p.lat},${p.lon}`)).size,
    first: first && { lat: round(first.lat, 3), lon: round(first.lon, 3), time: first.time },
    last: last && { lat: round(last.lat, 3), lon: round(last.lon, 3), time: last.time },
    ground_km: first && round(km(first, last)), ground_km_per_s: first && last.at > first.at ? round(km(first, last) / ((last.at - first.at) / 1000), 2) : null,
    dom_events: dom.length, dom_text_sample: dom[0]?.text?.replace(/\s+/g, ' ').trim() ?? null,
    dom_html_bytes: dom[0] ? Buffer.byteLength(dom[0].html) : null,
  },
  webhook: {
    deliveries: hooks.length, inbox_full: items.length >= 20, by_event: Object.fromEntries([...new Set(hooks.map((d) => d.event))].map((e) => [e, hooks.filter((d) => d.event === e).length])),
    signatures_valid: hooks.filter((d) => verify(secret, d.signature, d.body)).length,
    tampered_body_rejected: hooks.length > 0 && !verify(secret, hooks[0].signature, `${hooks[0].body} `),
    delivery_ids_unique_uuids: new Set(hooks.map((d) => d.delivery)).size === hooks.length && hooks.every((d) => /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(d.delivery || '')),
    job_id_matches: hooks.every((d) => d.json.job_id === id),
    events: hookEvents.length, events_by_type: byType(hookEvents), events_all_stored: hookEvents.every((e) => storedKeys.has(key(e))),
    stored_not_in_webhooks: stored.filter((e) => !hookKeys.has(key(e))).length,
    not_sent_are_the_last: stored.filter((e) => !hookKeys.has(key(e))).every((e) => hookEvents.every((h) => Date.parse(h.at) <= Date.parse(e.at))),
    signed_after_newest_event_seconds: delays.length ? { min: round(delays[0]), max: round(delays.at(-1)) } : null,
    min_gap_seconds: gaps.length ? secs(Math.min(...gaps)) : null, max_events_per_batch: Math.max(0, ...batches.map((d) => d.json.data?.events?.length || 0)),
    done: doneHook ? { status: doneHook.json.data?.status, progress: doneHook.json.data?.progress, error: doneHook.json.data?.error ?? null } : null,
  },
  measured_at: new Date().toISOString().slice(0, 10),
}, null, 2));
