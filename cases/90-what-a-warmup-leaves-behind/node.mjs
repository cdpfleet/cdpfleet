// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (a paid plan: jobs and profiles aren't on Community), PROXY_URL_US (a sticky US exit)
import { randomBytes } from 'node:crypto';
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL_US;
const API = 'https://cdpfleet.com';
const NAME = `case90-node-${randomBytes(4).toString('hex')}`; // a profile of its own for each run
// Three ordinary public sites: an encyclopedia, a scraping sandbox, a news front page.
const SITES = ['https://en.wikipedia.org/wiki/Main_Page', 'https://books.toscrape.com/', 'https://www.theguardian.com/us'];
const WARMUP = {
  url: SITES[0],
  sites: SITES.slice(1),
  browser: 'camoufox',
  profile: NAME,
  duration: 600, // a safety net: the program stops the job itself
  // No typing: the warmup would otherwise type into search boxes now and then.
  behavior: { scroll: true, mouse: true, typing: false, dwell_s: [4, 10], pages_per_site: [2, 4] },
};
const STOP_AFTER_S = 150;
const ENDED = ['done', 'failed', 'stopped', 'interrupted'];
const DAY = 86400;
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
  return { status: res.status, json, text };
}
const profileInfo = async () => (await api('GET', '/v1/me/profiles')).json?.profiles?.find((p) => p.name === NAME);

// The site a cookie or a host belongs to: its last two labels (three under co.uk and the like).
function site(host) {
  const labels = host.replace(/^\./, '').split('.');
  const n = /^(co|com|org|net|ac|gov)\.[a-z]{2}$/.test(labels.slice(-2).join('.')) ? 3 : 2;
  return labels.slice(-n).join('.');
}

// Cookies by site, first- vs third-party (first = a site the warmup visited as a page), and how long they last.
function summarize(cookies, firstParty, now) {
  const bySite = {};
  for (const c of cookies) bySite[site(c.domain)] = (bySite[site(c.domain)] || 0) + 1;
  const third = Object.entries(bySite).filter(([s]) => !firstParty.includes(s)).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const left = cookies.filter((c) => c.expires > 0).map((c) => (c.expires - now) / DAY);
  const keys = cookies.map((c) => `${c.name}|${c.domain}|${c.path}`);
  return {
    total: cookies.length,
    sites: Object.keys(bySite).length,
    first_party: cookies.filter((c) => firstParty.includes(site(c.domain))).length,
    third_party: cookies.length - cookies.filter((c) => firstParty.includes(site(c.domain))).length,
    third_party_sites: third.length,
    by_first_party_site: Object.fromEntries(firstParty.map((s) => [s, bySite[s] || 0])),
    top_third_party: third.slice(0, 5).map(([s, n]) => `${s} ${n}`),
    session: cookies.length - left.length,
    http_only: cookies.filter((c) => c.httpOnly).length,
    same_site_none: cookies.filter((c) => c.sameSite === 'None').length,
    expiry: {
      under_1_day: left.filter((d) => d < 1).length,
      under_30_days: left.filter((d) => d >= 1 && d < 30).length,
      under_1_year: left.filter((d) => d >= 30 && d < 365).length,
      a_year_or_more: left.filter((d) => d >= 365).length,
      max_days: left.length ? Math.round(Math.max(...left)) : null,
    },
    same_key_twice: keys.length - new Set(keys).size, // e.g. Firefox's per-site (partitioned) copies
  };
}

async function launch(profile) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch('https://starter.cdpfleet.com/camoufox/session', {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: PROXY, headless: true, ...(profile ? { profile } : {}) }),
    });
    if (res.ok) {
      const body = await res.json();
      const browser = await firefox.connect(body.wsUrl, { headers: { 'x-api-key': KEY } });
      return { sessionId: body.sessionId, browser };
    }
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await sleep((Number(res.headers.get('retry-after')) || 5) * 1000);
  }
}

// What one later session sees: the cookies it starts with, then for each warmed site one request for its
// /robots.txt — the Cookie header that first request carries is what the site gets from this browser, and
// the plain-text page lets us read the origin's localStorage and IndexedDB with none of the site's scripts run.
async function inspect(profile) {
  const { sessionId, browser } = await launch(profile);
  const t = Date.now();
  try {
    // With a profile, its context is browser.contexts()[0]; without one, a fresh context.
    const context = profile ? browser.contexts()[0] : await browser.newContext();
    const page = context.pages()[0] ?? await context.newPage();
    const atStart = await context.cookies();
    const origins = [];
    for (const url of SITES) {
      const origin = new URL(url).origin;
      let res = null;
      for (let attempt = 1; attempt <= 3 && !res; attempt++) {
        try { res = await page.goto(`${origin}/robots.txt`, { timeout: 60000 }); } catch (err) { if (attempt === 3) throw err; await sleep(2000); }
      }
      const header = (await res.request().allHeaders()).cookie || '';
      const names = header ? header.split('; ').map((c) => c.slice(0, c.indexOf('='))) : [];
      const storage = await page.evaluate(async () => ({ local_storage: localStorage.length, indexeddb: (await indexedDB.databases()).length }));
      origins.push({ origin, status: res.status(), cookie_header_cookies: names.length, cookie_header_names: names.slice(0, 8), ...storage });
      await sleep(3000);
    }
    return { session_id: sessionId, cookies_at_start: atStart, origins, seconds: secs(Date.now() - t) };
  } finally {
    await browser.close();
  }
}

const out = { measured_at: new Date().toISOString().slice(0, 10), sites: SITES, warmup: { ...WARMUP, profile: 'case90-node-…', stop_after_s: STOP_AFTER_S } };
let id = null;
try {
  // 1. Start the warmup into a new profile, on a sticky US exit.
  const t0 = Date.now();
  const start = await api('POST', '/v1/functions/warmup', { ...WARMUP, launch: { headless: true, proxy: PROXY } });
  if (start.status !== 202) throw new Error(`warmup ${start.status} ${start.text}`);
  id = start.json.job_id;
  out.warmup.start_status = start.status;

  // 2. Follow its progress; stop it after STOP_AFTER_S seconds.
  const samples = [];
  let job;
  let stop = null;
  for (;;) {
    await sleep(5000);
    job = (await api('GET', `/v1/jobs/${id}`)).json;
    samples.push({ s: Math.round((Date.now() - t0) / 1000), ...job.progress });
    if (ENDED.includes(job.status)) break;
    if (!stop && Date.now() - t0 >= STOP_AFTER_S * 1000) {
      const r = await api('DELETE', `/v1/jobs/${id}`);
      stop = { after_seconds: secs(Date.now() - t0), status: r.status, reply: r.json?.status ?? null, at: Date.now() };
    }
    if (Date.now() - t0 > (STOP_AFTER_S + 120) * 1000) throw new Error(`job still ${job.status}`);
  }
  out.job = {
    status: job.status, own_session: job.own_session, runs: job.runs, progress: job.progress, error: job.error,
    billed_seconds: job.billed_seconds, run_seconds: secs(Date.parse(job.ended_at) - Date.parse(job.started_at)),
    stop: stop && { after_seconds: stop.after_seconds, status: stop.status, reply: stop.reply, seconds_to_ended: secs(Date.parse(job.ended_at) - stop.at) },
    progress_samples: samples.filter((p, i) => p.visits !== undefined && (i === samples.length - 1 || p.visits !== samples[i + 1]?.visits)).map(({ s, visits, sites, challenges }) => ({ s, visits, sites, challenges })),
  };

  // 3. The visit history: the job's results log every page it browsed.
  const lines = [];
  for (let cursor = 0; cursor !== null;) {
    const r = (await api('GET', `/v1/jobs/${id}/results?cursor=${cursor}`)).json;
    lines.push(...r.results);
    if (r.next_cursor === cursor) break;
    cursor = r.next_cursor;
  }
  const visits = lines.filter((l) => l.type === 'visit');
  const firstParty = [...new Set(visits.map((v) => site(new URL(v.url).hostname)))].sort();
  out.visits = {
    count: visits.length, challenged: visits.filter((v) => v.challenged).length,
    by_site: Object.fromEntries(firstParty.map((s) => [s, visits.filter((v) => site(new URL(v.url).hostname) === s).length])),
    statuses: [...new Set(visits.map((v) => v.status))].sort(),
    dwell_s: visits.length ? { min: Math.min(...visits.map((v) => v.dwell_s)), max: Math.max(...visits.map((v) => v.dwell_s)) } : null,
    paths: visits.map((v) => { const u = new URL(v.url); return `${u.hostname}${u.pathname}`; }),
    cookie_lines: lines.filter((l) => l.type === 'cookies').length,
  };

  // 4. The cookies it left: GET /v1/jobs/{id}/cookies (after the end, the last set it delivered).
  const ck = await api('GET', `/v1/jobs/${id}/cookies`);
  const jobCookies = ck.json?.cookies || [];
  const now = Date.now() / 1000;
  out.job_cookies = { status: ck.status, live: ck.json?.live ?? null, at_vs_end_seconds: secs(Date.parse(ck.json?.at) - Date.parse(job.ended_at)), ...summarize(jobCookies, firstParty, now) };

  // 5. The profile, saved after the job's browser exited.
  let p;
  for (let i = 0; i < 60; i++) { p = await profileInfo(); if (p?.version >= 1 && !p.in_use) break; await sleep(2000); }
  out.profile = p ? { engine: p.engine, version: p.version, bytes: p.bytes, saved_after_end_seconds: secs(Date.parse(p.saved_at) - Date.parse(job.ended_at)) } : null;

  // 6. A later session on the profile (read-only: it sees the profile and doesn't change it), then a fresh one.
  const later = await inspect({ id: NAME, mode: 'readonly' });
  const keys = new Set(later.cookies_at_start.map((c) => `${c.name}|${c.domain}|${c.path}`));
  const jobKeys = new Set(jobCookies.map((c) => `${c.name}|${c.domain}|${c.path}`));
  const session = jobCookies.filter((c) => !(c.expires > 0));
  out.later = {
    cookies_at_start: summarize(later.cookies_at_start, firstParty, Date.now() / 1000),
    job_cookies_restored: [...jobKeys].filter((k) => keys.has(k)).length,
    job_cookies_distinct: jobKeys.size,
    session_cookies_restored: session.filter((c) => keys.has(`${c.name}|${c.domain}|${c.path}`)).length,
    session_cookies_in_job: session.length,
    not_in_job: [...keys].filter((k) => !jobKeys.has(k)).length,
    origins: later.origins,
    seconds: later.seconds,
  };
  const fresh = await inspect(null);
  out.fresh = { cookies_at_start: fresh.cookies_at_start.length, origins: fresh.origins, seconds: fresh.seconds };

  // 7. The bill: the warmup's own session and the two later sessions, from the session history.
  let sessions = [];
  for (let i = 0; i < 15; i++) {
    sessions = (await api('GET', `/v1/me/sessions?from=${encodeURIComponent(new Date(t0 - 60000).toISOString())}&limit=200`)).json?.sessions || [];
    if ([job.session_id, later.session_id, fresh.session_id].every((s) => sessions.some((x) => (x.session_id || x.id) === s && x.ended_at))) break;
    await sleep(2000);
  }
  const bill = (sid) => {
    const s = sessions.find((x) => (x.session_id || x.id) === sid);
    return s ? { engine: s.engine, weight: s.weight, seconds: s.ended_at ? secs(Date.parse(s.ended_at) - Date.parse(s.started_at)) : null } : null;
  };
  out.bill = { warmup: { billed_seconds: job.billed_seconds, session: bill(job.session_id) }, later: bill(later.session_id), fresh: bill(fresh.session_id) };
} finally {
  // Stop the job if something failed early, then delete the profile once nothing holds it.
  if (id && !out.job) await api('DELETE', `/v1/jobs/${id}`);
  for (let i = 0; i < 60 && (await profileInfo())?.in_use; i++) await sleep(2000);
  const del = await api('DELETE', `/v1/me/profiles/${NAME}`);
  out.cleanup = { delete_status: del.status, deleted: del.json?.deleted === true && (await profileInfo()) === undefined };
}
console.log(JSON.stringify(out, null, 2));
