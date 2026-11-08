// Node.js 18+, no packages. env: CDPFLEET_API_KEY (five countries in one call hold five threads: a paid plan)
const KEY = process.env.CDPFLEET_API_KEY;
const API = 'https://cdpfleet.com';
const COUNTRIES = ['us', 'de', 'gb', 'jp', 'br']; // the first one is the baseline the diff compares with
const FORMATS = ['markdown', 'html'];
const MAX_WAIT = 45;
const PAGES = [
  ['Steam store page', 'https://store.steampowered.com/app/620/Portal_2/'],
  ['Google home page', 'https://www.google.com/'],
  ['Nike home page', 'https://www.nike.com/'],
  ['example.com (control)', 'https://example.com/'],
];
// Prices in the five countries' currencies: R$ before $, the symbol before or after the number.
// Plain [0-9] and [ \u00a0] rather than \d and \s, so the pattern means the same in all five languages.
const PRICE = /R\$[ \u00a0]?[0-9][0-9.,]*|US\$[ \u00a0]?[0-9][0-9.,]*|£[ \u00a0]?[0-9][0-9.,]*|€[ \u00a0]?[0-9][0-9.,]*|[0-9][0-9.,]*[ \u00a0]?€|[¥￥][ \u00a0]?[0-9][0-9,]*|[0-9][0-9,]*[ \u00a0]?円|\$[ \u00a0]?[0-9][0-9.,]*/g;
// The "accept all" button of a cookie banner, in the five countries' languages.
const CONSENT = /Accept all|Alle akzeptieren|Aceitar todos|Aceitar tudo|すべて同意|すべて受け入れる|すべて許可/i;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const bytes = (s) => (typeof s === 'string' ? Buffer.byteLength(s) : 0);
const since = new Date(Date.now() - 60000).toISOString();

const prices = (text) => (text.match(PRICE) || []).map((p) => p.replace(/[.,]$/, '').replace('\u00a0', ' '));
function currency(p) {
  if (p.startsWith('R$')) return 'BRL';
  if (p.includes('£')) return 'GBP';
  if (p.includes('€')) return 'EUR';
  if (/[¥￥円]/.test(p)) return 'JPY';
  return 'USD';
}
const attr = (html, re) => (re.exec(html) || [])[1] ?? null;

// What the Function's unified diff (one country against the baseline) says.
function readDiff(d) {
  if (typeof d !== 'string') return null;
  const lines = d.split('\n');
  const added = lines.filter((l) => l.startsWith('+') && !l.startsWith('+++'));
  const removed = lines.filter((l) => l.startsWith('-') && !l.startsWith('---'));
  const was = removed.flatMap(prices);
  const now = added.flatMap(prices);
  const changes = [];
  for (let i = 0; i < Math.min(was.length, now.length) && changes.length < 3; i++) if (was[i] !== now[i]) changes.push(`${was[i]} → ${now[i]}`);
  return { bytes: bytes(d), hunks: lines.filter((l) => l.startsWith('@@')).length, added: added.length, removed: removed.length, price_changes: changes };
}

// The call in the account's history (its x-call-id is the session id there): weight and billed thread-seconds.
async function billed(id) {
  for (let i = 0; i < 30 && id; i++) {
    const r = await fetch(`${API}/v1/me/sessions?from=${encodeURIComponent(since)}&limit=200`, { headers: { 'x-api-key': KEY } });
    const s = (await r.json()).sessions?.find((x) => x.session_id === id);
    if (s?.ended_at) return { engine: s.engine, weight: s.weight, thread_seconds: Math.round((Date.parse(s.ended_at) - Date.parse(s.started_at)) * s.weight / 100) / 10 };
    await sleep(2000);
  }
  return null;
}

async function geo(url) {
  const t = Date.now();
  const res = await fetch(`${API}/v1/functions/geo`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ url, countries: COUNTRIES, formats: FORMATS, max_wait: MAX_WAIT }),
    signal: AbortSignal.timeout((MAX_WAIT + 30) * 1000),
  });
  const text = await res.text();
  let body = {};
  try { body = JSON.parse(text); } catch { /* not JSON */ }
  return { res, text, body, ms: Date.now() - t };
}
const loaded = (r) => r?.status >= 200 && r?.status < 400;

const pages = [];
for (const [name, url] of PAGES) { // one call per page, one after another
  let call = await geo(url);
  // Every diff is against the first country: if that one failed, the call is retried once.
  const first = call;
  if (call.res.status === 200 && !loaded(call.body.results?.[COUNTRIES[0]])) call = await geo(url);
  const { res, text, body, ms } = call;
  const results = body.results || {};
  const base = results[COUNTRIES[0]] || {};
  const countries = {};
  for (const c of COUNTRIES) {
    const r = results[c] || {};
    const html = r.html || '';
    const md = r.markdown || '';
    const found = prices(md);
    countries[c] = {
      status: r.status ?? null,
      error: r.error ?? null,
      partial: r.partial === true,
      lang: attr(html, /<html[^>]*\slang="([^"]+)"/i),
      canonical: attr(html, /<link[^>]*rel="canonical"[^>]*href="([^"]+)"/i) ?? attr(html, /<link[^>]*href="([^"]+)"[^>]*rel="canonical"/i),
      title: attr(html, /<title[^>]*>([^<]*)</i)?.trim() ?? null,
      currencies: [...new Set(found.map(currency))],
      prices: found.slice(0, 4),
      consent_banner: html ? CONSENT.test(html) : null,
      markdown_bytes: bytes(md),
      html_bytes: bytes(html),
      same_markdown_as_baseline: c === COUNTRIES[0] || !md || !base.markdown ? null : md === base.markdown,
      diff: c === COUNTRIES[0] ? null : { markdown: readDiff(body.diff?.markdown?.[c]), html_bytes: bytes(body.diff?.html?.[c]) },
    };
  }
  pages.push({
    page: name,
    url,
    attempts: call === first ? 1 : 2,
    first_attempt: call === first ? null : {
      call_id: first.res.headers.get('x-call-id'),
      errors: Object.fromEntries(COUNTRIES.filter((c) => !loaded(first.body.results?.[c])).map((c) => [c, first.body.results?.[c]?.error ?? null])),
      diffs: Object.fromEntries(Object.entries(first.body.diff?.markdown || {}).map(([c, d]) => [c, bytes(d)])),
    },
    status: res.status,
    error: body.error ?? null,
    seconds: Math.round(ms / 100) / 10,
    call_id: res.headers.get('x-call-id'),
    warning: res.headers.get('x-cdpfleet-warning'),
    response_bytes: bytes(text),
    diff_bytes: { markdown: Object.values(body.diff?.markdown || {}).reduce((a, d) => a + bytes(d), 0), html: Object.values(body.diff?.html || {}).reduce((a, d) => a + bytes(d), 0) },
    ok_countries: COUNTRIES.filter((c) => loaded(results[c])),
    countries,
  });
}
for (const p of pages) {
  p.billed = await billed(p.call_id);
  if (p.first_attempt) p.first_attempt.billed = await billed(p.first_attempt.call_id);
}

console.log(JSON.stringify({ language: 'node', countries: COUNTRIES, formats: FORMATS, max_wait: MAX_WAIT, pages, measured_at: new Date().toISOString().slice(0, 10) }, null, 2));
