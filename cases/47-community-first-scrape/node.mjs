// npm install playwright@1.60.0
// env: COMMUNITY_API_KEY
import { chromium } from 'playwright';

const KEY = process.env.COMMUNITY_API_KEY;

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return res.json();
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// The exit the site sees. Shared proxies drop the odd connection: retry a few times.
async function exitOf(page) {
  for (let i = 1; ; i++) {
    try {
      await page.goto('http://ip-api.com/json', { timeout: 30000 });
      const j = JSON.parse(await page.textContent('body'));
      return { ip: j.query, country: j.countryCode };
    } catch (err) {
      if (i === 4) throw err;
      await page.waitForTimeout(2000 * i);
    }
  }
}

async function session(body, work) {
  const s = await launch({ headless: true, record: false, ...body });
  const browser = await chromium.connect(s.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    return { weight: s.weight, ...(await work(await browser.newPage())) };
  } finally {
    await browser.close(); // ends the session and stops the clock
  }
}

// 1. What the free plan includes.
const me = await (await fetch('https://cdpfleet.com/v1/me', { headers: { 'x-api-key': KEY } })).json();
const sub = me.subscription;
const plan = {
  plan: sub.plan_id, threads: sub.threads, minutes_per_thread_per_day: sub.daily_minutes_per_thread,
  max_session_minutes: sub.max_session_seconds / 60, max_pages: sub.features.max_pages, memory_mb: sub.features.memory_mb,
  proxy_options: sub.plan_proxy?.options,
};

// 2. No proxy in the launch: the plan's included proxy is used. Scrape a catalogue page.
const first = await session({}, async (page) => {
  const exit = await exitOf(page);
  await page.goto('https://books.toscrape.com/', { timeout: 45000 });
  const books = await page.$$eval('article.product_pod', (els) => els.map((e) => ({
    title: e.querySelector('h3 a').getAttribute('title'),
    price: e.querySelector('.price_color').textContent.trim(),
  })));
  return { exit, books: books.length, first_book: books[0] };
});

// 3. A cdpfleet-plan token picks a country and keeps the same exit across sessions.
const token = `cdpfleet-plan-country-de-session-c47${Math.random().toString(36).slice(2, 7)}-lifetime-10`;
const a = await session({ proxy: token }, async (page) => ({ exit: await exitOf(page) }));
const b = await session({ proxy: token }, async (page) => ({ exit: await exitOf(page) }));

console.log(JSON.stringify({
  plan,
  default_proxy: { weight: first.weight, exit_country: first.exit.country, exit_ip: first.exit.ip, books: first.books, first_book: first.first_book },
  plan_token: { token: token.replace(/session-\w+/, 'session-…'), first_country: a.exit.country, second_country: b.exit.country, same_ip: a.exit.ip === b.exit.ip },
}, null, 2));
