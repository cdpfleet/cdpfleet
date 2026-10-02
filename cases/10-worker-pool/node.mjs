// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PAGES = 24;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Launch with the retries the API asks for: 429 (thread limit, launch rate) and 503
// (fleet momentarily busy) carry Retry-After.
async function launch(stats) {
  for (let attempt = 1; ; attempt++) {
    const t = Date.now();
    const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
    });
    if (res.ok) return { ...(await res.json()), launchMs: Date.now() - t };
    const body = await res.json().catch(() => ({}));
    const retryable = res.status === 503 || (res.status === 429 && !/quota/.test(body.error));
    if (!retryable || attempt === 10) throw new Error(`launch: ${res.status} ${body.error}`);
    stats.retries[body.error] = (stats.retries[body.error] || 0) + 1;
    await sleep(Number(res.headers.get('retry-after') || 2) * 1000);
  }
}

// Residential proxies drop a tunnel now and then (ERR_TUNNEL_CONNECTION_FAILED): retry.
async function scrape(page, stats) {
  for (let attempt = 1; ; attempt++) {
    try {
      await page.goto('https://en.wikipedia.org/wiki/Special:Random', { timeout: 30000 });
      return page.title();
    } catch (err) {
      stats.pageRetries++;
      if (attempt === 3) return `(failed: ${err.message.split('\n')[0]})`;
    }
  }
}

// Runs `jobs` pages on `workers` parallel workers; each worker either opens one session
// and reuses it, or opens a new session for every page.
async function run(workers, reuse) {
  const stats = { launches: 0, launchMs: 0, connectMs: 0, sessionSeconds: 0, titles: [], retries: {}, pageRetries: 0, connectFailures: 0 };
  let next = 0;
  // If the connect fails (rare: the server holding the browser didn't answer), don't
  // reconnect to the same wsUrl — launch a fresh session. Such sessions aren't billed.
  const openSession = async () => {
    for (let attempt = 1; ; attempt++) {
      const s = await launch(stats);
      stats.launches++;
      stats.launchMs += s.launchMs;
      const t = Date.now();
      try {
        const browser = await chromium.connect(s.wsUrl, { headers: { 'x-api-key': KEY } });
        stats.connectMs += Date.now() - t;
        return { browser, started: t };
      } catch (err) {
        stats.connectFailures++;
        if (attempt === 3) throw err;
      }
    }
  };
  const closeSession = async ({ browser, started }) => {
    await browser.close();
    stats.sessionSeconds += (Date.now() - started) / 1000;
  };
  const worker = async () => {
    if (reuse) {
      const session = await openSession();
      try {
        const page = await session.browser.newPage();
        while (next < PAGES) { next++; stats.titles.push(await scrape(page, stats)); }
      } finally {
        await closeSession(session);
      }
      return;
    }
    while (next < PAGES) {
      next++;
      const session = await openSession();
      try {
        stats.titles.push(await scrape(await session.browser.newPage(), stats));
      } finally {
        await closeSession(session);
      }
    }
  };
  const t = Date.now();
  await Promise.all(Array.from({ length: workers }, worker));
  return {
    strategy: reuse ? 'reuse one session per worker' : 'new session per page',
    pages: stats.titles.length,
    wall_seconds: Math.round((Date.now() - t) / 100) / 10,
    launches: stats.launches,
    avg_launch_ms: Math.round(stats.launchMs / stats.launches),
    avg_connect_ms: Math.round(stats.connectMs / stats.launches),
    launch_retries: stats.retries,
    connect_failures: stats.connectFailures,
    page_retries: stats.pageRetries,
    billed_thread_seconds: Math.round(stats.sessionSeconds),
    sample_titles: stats.titles.slice(0, 3),
  };
}

// Size the pool from the plan: never more workers than threads.
const me = await (await fetch('https://cdpfleet.com/v1/me', { headers: { 'x-api-key': KEY } })).json();
const workers = Math.min(me.subscription.threads, 6);
console.log(JSON.stringify({
  plan_threads: me.subscription.threads,
  workers,
  results: [await run(workers, false), await run(workers, true)],
}, null, 2));
