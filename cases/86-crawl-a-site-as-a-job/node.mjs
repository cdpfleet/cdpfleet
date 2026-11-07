// Node.js 18+, no packages. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
import crypto from 'node:crypto';
import zlib from 'node:zlib';

const KEY = process.env.CDPFLEET_API_KEY;
const API = 'https://cdpfleet.com';
const SITE = 'https://books.toscrape.com/'; // a sandbox made for scraping practice
const CRAWL = { max_depth: 2, max_pages: 100, concurrency: 4, delay_ms: 250, formats: ['markdown'] };
const EXTRACT = { title: 'h1', price: '.product_main .price_color' };
const STOP_AT = 30; // stop the job once this many pages are done, then resume it
const ENDED = ['done', 'failed', 'stopped', 'interrupted'];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const secs = (ms) => Math.round(ms / 100) / 10;

async function api(method, path, body) {
  const res = await fetch(`${API}${path}`, {
    method,
    headers: { 'x-api-key': KEY, ...(body ? { 'content-type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch { /* not JSON */ }
  return { status: res.status, json, text, headers: res.headers };
}
// Poll the job every 3 s until `until(job)` holds.
async function waitFor(id, until, ms) {
  let job;
  for (const end = Date.now() + ms; Date.now() < end; await sleep(3000)) {
    job = (await api('GET', `/v1/jobs/${id}`)).json;
    if (job && until(job)) return job;
  }
  throw new Error(`job ${id}: still ${job?.status} after ${ms / 1000} s`);
}
async function stored(id) { // results stored so far, part by part
  const rows = [];
  for (let cursor = 0; cursor !== null;) {
    const r = (await api('GET', `/v1/jobs/${id}/results?cursor=${cursor}`)).json;
    rows.push(...r.results);
    if (r.next_cursor === cursor) break; // nothing new yet
    cursor = r.next_cursor;
  }
  return rows;
}

// A zip's file names, and one file's bytes (Node has no unzip of its own; zip64-aware).
function unzip(buf, want) {
  const eocd = buf.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]));
  let count = buf.readUInt16LE(eocd + 10);
  let at = buf.readUInt32LE(eocd + 16);
  if (count === 0xffff || at === 0xffffffff) {
    const z = Number(buf.readBigUInt64LE(eocd - 20 + 8)); // zip64 end record
    count = Number(buf.readBigUInt64LE(z + 32));
    at = Number(buf.readBigUInt64LE(z + 48));
  }
  const names = [];
  let data = null;
  for (let i = 0; i < count; i++) {
    let comp = buf.readUInt32LE(at + 20);
    let local = buf.readUInt32LE(at + 42);
    const method = buf.readUInt16LE(at + 10);
    const nameLen = buf.readUInt16LE(at + 28);
    const extraLen = buf.readUInt16LE(at + 30);
    const name = buf.toString('utf8', at + 46, at + 46 + nameLen);
    for (let x = at + 46 + nameLen; x < at + 46 + nameLen + extraLen; x += 4 + buf.readUInt16LE(x + 2)) {
      if (buf.readUInt16LE(x) !== 1) continue; // zip64 sizes and offset
      let p = x + 4;
      if (buf.readUInt32LE(at + 24) === 0xffffffff) p += 8;
      if (comp === 0xffffffff) { comp = Number(buf.readBigUInt64LE(p)); p += 8; }
      if (local === 0xffffffff) local = Number(buf.readBigUInt64LE(p));
    }
    names.push(name);
    if (name === want) {
      const start = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
      const raw = buf.subarray(start, start + comp);
      data = method === 8 ? zlib.inflateRawSync(raw) : raw;
    }
    at += 46 + nameLen + extraLen + buf.readUInt16LE(at + 32);
  }
  return { names, data };
}

// 1. Map: the site's URLs in one instant call (sitemaps + the start page's links).
let t = Date.now();
const map = await api('POST', '/v1/functions/map', { url: SITE, max_urls: 5000 });
const mapOut = {
  status: map.status, seconds: secs(Date.now() - t), urls: map.json?.urls?.length ?? null,
  sources: map.json?.sources ?? null, partial: map.headers.get('x-cdpfleet-partial'),
  categories: (map.json?.urls || []).filter((u) => u.includes('/catalogue/category/')).length,
};

// 2. Start the crawl as a job: a webhook when it's done, and a zip archive at the end.
// The receiver here is cdpfleet.com's own test inbox (keeps deliveries an hour, read back with GET);
// use your own public https endpoint.
const hook = `${API}/e2e/hook/${crypto.randomBytes(18).toString('base64url')}`;
const tStart = Date.now();
const start = await api('POST', '/v1/functions/crawl', {
  url: SITE, ...CRAWL, extract: EXTRACT,
  deliver: { webhook: { url: hook, events: ['done'] }, format: 'archive' },
});
if (start.status !== 202) throw new Error(`crawl ${start.status} ${start.text}`);
const id = start.json.job_id;

// 3. Watch it, and stop it part-way.
const before = await waitFor(id, (j) => (j.progress?.done || 0) >= STOP_AT || ENDED.includes(j.status), 600000);
const stopReply = await api('DELETE', `/v1/jobs/${id}`);
const tStop = Date.now();
const stopped = await waitFor(id, (j) => ENDED.includes(j.status), 300000);
const tStopped = Date.now();
const atStop = await stored(id);
const hooksAtStop = (await (await fetch(hook)).json()).items;
const archiveAtStop = (await api('GET', `/v1/jobs/${id}/archive`)).json;

// 4. Resume: a new run of the same job, from its checkpoint.
const tResume = Date.now();
const resume = await api('POST', `/v1/jobs/${id}/resume`);
// 5. Wait for the second run to end.
const done = await waitFor(id, (j) => j.runs >= 2 && ENDED.includes(j.status), 900000);
const tDone = Date.now();

// 6a. Every result as one JSON-lines file.
const jsonl = await (await fetch(`${API}/v1/jobs/${id}/results.jsonl`, { headers: { 'x-api-key': KEY } })).text();
const rows = jsonl.trim().split('\n').map((l) => JSON.parse(l));
const urls = new Set(rows.map((r) => r.url));
const firstRun = new Set(atStop.map((r) => r.url));

// 6b. The same results as a zip (built when the job ended).
const archive = await waitFor(id, (j) => ['ready', 'failed'].includes(j.archive?.status), 300000)
  .then(() => api('GET', `/v1/jobs/${id}/archive`)).then((r) => r.json);
const zip = Buffer.from(await (await fetch(archive.url)).arrayBuffer()); // a storage link, no key
const { names, data: index } = unzip(zip, 'index.jsonl');

// 7. The webhooks: every delivery is signed with the account's secret, as HMAC-SHA256(secret, "<t>.<raw body>").
const secret = (await api('GET', '/v1/me/webhook-secret')).json.secret;
const check = (d) => {
  const { t: ts, v1 } = Object.fromEntries((d.signature || '').split(',').map((kv) => kv.split('=')));
  const body = JSON.parse(d.body);
  return {
    event: d.event, status: body.data?.status ?? null, job_id_matches: body.job_id === id,
    delivery_id_is_uuid: /^[0-9a-f-]{36}$/.test(d.delivery || ''),
    signature_valid: Boolean(v1) && v1 === crypto.createHmac('sha256', secret).update(`${ts}.${d.body}`).digest('hex'),
    signed_at: Number(ts), data: body.data,
  };
};
let deliveries = [];
for (const end = Date.now() + 90000; Date.now() < end; await sleep(3000)) {
  deliveries = (await (await fetch(hook)).json()).items.map(check);
  if (deliveries.some((d) => d.event === 'job.done' && d.status === 'done')) break;
}
const final = deliveries.find((d) => d.event === 'job.done' && d.status === 'done');

// 8. What it cost: a run holds `concurrency` threads, billed_seconds is the total in thread-seconds.
const runSeconds = done.billed_seconds / done.concurrency;

console.log(JSON.stringify({
  language: 'node',
  site: SITE,
  map: mapOut,
  crawl: { ...CRAWL, extract: Object.keys(EXTRACT), start_status: start.status },
  stop: {
    requested_at_done: before.progress?.done ?? 0, reply: stopReply.json?.status ?? stopReply.status,
    seconds_to_stopped: secs(tStopped - tStop), status: stopped.status, progress: stopped.progress,
    stored_results: atStop.length, webhooks_by_then: hooksAtStop.map((d) => `${d.event} ${JSON.parse(d.body).data?.status}`), archive: archiveAtStop?.status ?? null,
  },
  resume: { status: resume.status, reply: resume.json?.status ?? null },
  job: {
    status: done.status, runs: done.runs, progress: done.progress, concurrency: done.concurrency,
    billed_seconds: done.billed_seconds, results: { parts: done.results?.parts, bytes: done.results?.bytes },
    started_at: done.started_at, ended_at: done.ended_at, expires_at: done.expires_at,
  },
  results_jsonl: {
    lines: rows.length, distinct_urls: urls.size, bytes: Buffer.byteLength(jsonl),
    status_200: rows.filter((r) => r.status === 200).length,
    errors: rows.filter((r) => r.error).map((r) => ({ url: r.url, status: r.status ?? null, error: String(r.error).slice(0, 120) })),
    with_markdown: rows.filter((r) => typeof r.markdown === 'string' && r.markdown.length > 0).length,
    books_with_price: rows.filter((r) => typeof r.extract?.price === 'string').length,
    category_pages: rows.filter((r) => r.url.includes('/catalogue/category/')).length,
    depths: Object.fromEntries([...new Set(rows.map((r) => r.depth))].sort().map((d) => [d, rows.filter((r) => r.depth === d).length])),
    first_run: firstRun.size, second_run_new: [...urls].filter((u) => !firstRun.has(u)).length,
    sample: rows.filter((r) => r.extract?.price).slice(0, 2).map((r) => ({ url: r.url, title: r.extract.title, price: r.extract.price })),
  },
  archive: {
    status: archive.status, bytes: archive.bytes, downloaded_bytes: zip.length, files: names.length,
    markdown_files: names.filter((n) => n.endsWith('/index.md')).length,
    extract_files: names.filter((n) => n.endsWith('/extract.json')).length,
    index_lines: index ? index.toString('utf8').trim().split('\n').length : 0,
    has: ['index.jsonl', 'errors.jsonl', 'job.json'].filter((n) => names.includes(n)),
  },
  webhook: {
    deliveries: deliveries.map(({ data, signed_at, ...d }) => d),
    final_done: final ? { data: final.data, signed_seconds_after_end: final.signed_at - Math.floor(Date.parse(done.ended_at) / 1000) } : null,
  },
  cost: {
    billed_thread_seconds: done.billed_seconds, pages: rows.length,
    run_seconds: Math.round(runSeconds * 10) / 10,
    pages_per_minute: Math.round(rows.length / (runSeconds / 60) * 10) / 10,
    thread_seconds_per_page: Math.round(done.billed_seconds / rows.length * 10) / 10,
    client_running_seconds: secs(tStopped - tStart + tDone - tResume),
  },
  measured_at: new Date().toISOString().slice(0, 10),
}, null, 2));
