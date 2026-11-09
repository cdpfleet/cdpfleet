// Node.js 18+, no packages. env: CDPFLEET_API_KEY (a paid plan: monitors aren't on Community)
import crypto from 'node:crypto';

const KEY = process.env.CDPFLEET_API_KEY;
const API = 'https://cdpfleet.com';
const PAGE = 'https://quotes.toscrape.com/random'; // a practice site: a different quote on every load
const MAX_RUNS = 5; // checks to trigger at most while waiting for a change (the same quote can come up twice)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const secs = (ms) => Math.round(ms / 100) / 10;
const since = new Date(Date.now() - 60000).toISOString();

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

// x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>. Check it against the raw
// bytes received, before parsing them, and refuse old timestamps (replays).
function verify(secret, header, rawBody, now = Date.now()) {
  const { t, v1 } = Object.fromEntries((header || '').split(',').map((kv) => kv.split('=')));
  if (!t || !v1) return false;
  const expected = crypto.createHmac('sha256', secret).update(`${t}.${rawBody}`).digest();
  const given = Buffer.from(v1, 'hex');
  return given.length === expected.length && crypto.timingSafeEqual(given, expected) && Math.abs(now / 1000 - Number(t)) < 300;
}

// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour, raw body and
// headers, and reads them back with GET. Use your own public https endpoint.
const hook = `${API}/e2e/hook/${crypto.randomBytes(18).toString('base64url')}`;
const inbox = async () => (await (await fetch(hook)).json()).items;
const secret = (await api('GET', '/v1/me/webhook-secret')).json.secret;

// 1. Create the monitor: only the quote element, as text; the tags dropped (remove) and the "(about)" link
// label taken out of the text (ignore) before comparing.
// every: "1h" is its own schedule; the program doesn't wait for it and triggers checks with POST …/run.
const spec = { url: PAGE, format: 'text', selector: '.quote', remove: ['.tags'], ignore: ['\\(about\\)'], every: '1h', deliver: { webhook: { url: hook } } };
const created = await api('POST', '/v1/monitors', spec);
if (created.status !== 201) throw new Error(`create ${created.status} ${created.text}`);
const id = created.json.monitor_id;
const out = { language: 'node', page: PAGE };
try {
  const listed = (await api('GET', '/v1/monitors')).json.monitors.some((m) => m.monitor_id === id);
  out.monitor = {
    create_status: created.status, every: created.json.every, format: created.json.format, selector: created.json.selector,
    remove: created.json.remove, ignore: created.json.ignore, webhook: created.json.deliver?.webhook?.url === hook, active: created.json.active, listed,
    first_check_due_seconds: secs(Date.parse(created.json.next_run_at) - Date.parse(created.json.created_at)),
  };

  // 2. Check now, and again until the page has changed. Poll the monitor and the inbox every 0.5 s.
  const checks = [];
  let change = null;
  for (let n = 1; n <= MAX_RUNS && !change; n++) {
    const tRun = Date.now();
    const run = await api('POST', `/v1/monitors/${id}/run`);
    let m;
    let tStatus = null;
    let item = null;
    let tHook = null;
    for (const end = Date.now() + 120000; Date.now() < end; await sleep(500)) {
      if (!tStatus) {
        m = (await api('GET', `/v1/monitors/${id}`)).json;
        if (m.checks >= n) tStatus = Date.now();
      }
      if (tStatus && m.last_status !== 'changed') break;
      if (!item) {
        item = (await inbox()).find((d) => d.event === 'monitor.change') || null;
        if (item) tHook = Date.now();
      }
      if (tStatus && item) break;
    }
    if (!tStatus) throw new Error(`check ${n}: no result after 120 s`);
    checks.push({
      n, run_status: run.status, status: m.last_status, error: m.last_error,
      seconds_to_status: secs(tStatus - tRun), check_seconds: secs(Date.parse(m.last_run_at) - tRun),
      t_run: tRun, last_run_at: m.last_run_at,
    });
    if (m.last_status === 'changed') change = { n, tRun, item, tHook };
  }

  // 3. The webhook: verify it, then compare it with the change as the API keeps it.
  const deliveries = await inbox();
  const d = change?.item;
  if (d) {
    const body = JSON.parse(d.body);
    const data = body.data || {};
    const t = Number(/t=(\d+)/.exec(d.signature || '')?.[1]);
    const full = (await api('GET', `/v1/monitors/${id}/changes/${data.change_id}`)).json;
    const before = full.before_url ? await (await fetch(full.before_url)).text() : null;
    const after = await (await fetch(full.after_url)).text();
    const snapshot = await (await fetch(data.snapshot_url)).text();
    const lines = data.diff.split('\n');
    out.webhook = {
      deliveries: deliveries.length, event_header: d.event, type: body.type,
      delivery_id_is_uuid: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(d.delivery || ''),
      signature_valid: verify(secret, d.signature, d.body),
      tampered_body_rejected: !verify(secret, d.signature, d.body.replace('"added":', '"added": ')),
      monitor_id_matches: data.monitor_id === id, url: data.url, keys: Object.keys(data).sort(),
      seconds_run_to_seen: secs(change.tHook - change.tRun),
      seconds_run_to_signed: t - Math.floor(change.tRun / 1000),
      signed_within_1s_of_change: Math.abs(t * 1000 - Date.parse(data.at)) < 1000,
    };
    out.change = {
      after_runs: change.n, change_id: data.change_id, added: data.added, removed: data.removed,
      diff_bytes: Buffer.byteLength(data.diff), diff_lines: lines.length,
      removed_lines: lines.filter((l) => /^-(?!--)/.test(l)).map((l) => l.slice(1)),
      added_lines: lines.filter((l) => /^\+(?!\+\+)/.test(l)).map((l) => l.slice(1)),
      diff_has_ignored_text: /Tags:|\(about\)/.test(data.diff),
      api_diff_equal: full.diff === data.diff, api_counts_equal: full.added === data.added && full.removed === data.removed,
      snapshot_equals_after: snapshot === after, before_lines: before?.split('\n').length ?? null, after_lines: after.split('\n').length,
      after_text: after,
    };
  }

  // 4. The history: every change, newest first, with a preview of its diff.
  const history = (await api('GET', `/v1/monitors/${id}/changes`)).json;
  const m = (await api('GET', `/v1/monitors/${id}`)).json;
  out.history = {
    changes: history.changes.length, change_ids: history.changes.map((c) => c.change_id),
    newest_is_webhook_change: history.changes[0]?.change_id === out.change?.change_id,
    monitor_checks: m.checks, monitor_changes: m.changes, last_status: m.last_status,
    next_run_minutes_after_last_check: Math.round((Date.parse(m.next_run_at) - Date.parse(m.last_run_at)) / 60000),
  };

  // 5. Pause it (PATCH active: false): no more scheduled checks, history kept.
  const paused = await api('PATCH', `/v1/monitors/${id}`, { active: false });
  out.pause = { status: paused.status, active: paused.json?.active, next_run_at: paused.json?.next_run_at ?? null };

  // 6. What the checks cost. Each check is a scrape the service runs as you, billed like any call: it has no
  // x-call-id you can see, but it is in the account's session history as engine "function:scrape".
  await sleep(3000);
  const sessions = (await api('GET', `/v1/me/sessions?from=${encodeURIComponent(since)}&limit=200`)).json.sessions;
  out.billing = checks.map((c) => {
    const inside = sessions.filter((s) => s.engine === 'function:scrape' && Date.parse(s.started_at) >= c.t_run - 2000 && Date.parse(s.started_at) <= Date.parse(c.last_run_at))
      .sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at));
    const s = inside[0]; // the first scrape to start after the run request
    return {
      check: c.n, sessions_in_window: inside.length, engine: s?.engine ?? null, weight: s?.weight ?? null,
      seconds: s?.ended_at ? secs(Date.parse(s.ended_at) - Date.parse(s.started_at)) : null,
      thread_seconds: s?.ended_at ? secs((Date.parse(s.ended_at) - Date.parse(s.started_at)) * s.weight) : null,
    };
  });
  out.checks = checks.map(({ t_run, last_run_at, ...c }) => c);
} finally {
  // 7. Delete it, whatever happened above.
  const del = await api('DELETE', `/v1/monitors/${id}`);
  const after = await api('GET', `/v1/monitors/${id}`);
  out.deleted = { status: del.status, deleted: del.json?.deleted === true, get_after: after.status };
}

out.measured_at = new Date().toISOString().slice(0, 10);
console.log(JSON.stringify(out, null, 2));
