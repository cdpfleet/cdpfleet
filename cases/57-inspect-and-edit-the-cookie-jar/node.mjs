// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;
const SITE = 'https://www.httpbin.org';

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
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

async function open(engine) {
  const session = await launch(engine, { proxy: PROXY, headless: true, record: false });
  const type = { chromium, firefox, webkit }[engine];
  return type.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
}

// httpbin answers 502/503 now and then: retry a page that isn't JSON.
async function json(page, url) {
  for (let attempt = 1; ; attempt++) {
    await page.goto(url, { timeout: 45000 });
    try { return JSON.parse(await page.textContent('body')); } catch (err) {
      if (attempt === 4) throw err;
      await new Promise((r) => setTimeout(r, 3000));
    }
  }
}
const names = (cookies) => cookies.map((c) => c.name).sort();

// Part 1: inspect, edit, clear and carry the jar (Chromium).
const jar = {};
let saved;
{
  const browser = await open('chromium');
  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    // The site sets two cookies (no expiry: session cookies); another site sets one more.
    await json(page, `${SITE}/cookies/set?session_token=abc123&theme=dark`);
    await context.addCookies([{ name: 'other_site', value: '1', url: 'https://example.com' }]);
    jar.all_cookies = names(await context.cookies());
    const mine = await context.cookies(SITE); // only the cookies that would be sent to this URL
    jar.for_site = names(mine);
    jar.session_cookie_expires = mine.find((c) => c.name === 'session_token').expires; // -1 = session cookie

    // Edit: addCookies with the same name, domain and path replaces the cookie.
    const weekFromNow = Math.floor(Date.now() / 1000) + 7 * 86400;
    await context.addCookies([
      { ...mine.find((c) => c.name === 'session_token'), expires: weekFromNow },
      { ...mine.find((c) => c.name === 'theme'), value: 'light' },
    ]);
    const edited = await context.cookies(SITE);
    jar.token_expires_in_days = Math.round((edited.find((c) => c.name === 'session_token').expires - Date.now() / 1000) / 86400);
    jar.server_sees_after_edit = (await json(page, `${SITE}/cookies`)).cookies;

    // Clear one cookie by name (other filters: domain, path).
    await context.clearCookies({ name: 'theme' });
    jar.after_clear_by_name = names(await context.cookies());

    // Carry the login as plain JSON: just the site's cookies.
    saved = JSON.stringify(await context.cookies(SITE));
    jar.saved_json_bytes = saved.length;
  } finally {
    await browser.close();
  }
}
{
  const browser = await open('chromium'); // a new session on another browser
  try {
    const context = await browser.newContext();
    jar.new_session_before = names(await context.cookies());
    await context.addCookies(JSON.parse(saved));
    const page = await context.newPage();
    jar.new_session_server_sees = (await json(page, `${SITE}/cookies`)).cookies;
  } finally {
    await browser.close();
  }
}

// Part 2: the same three ways to set a cookie, on three hosts, in three engines.
// httpbin.org and postman-echo.com are on the Public Suffix List; www.httpbin.org isn't.
const HOSTS = ['httpbin.org', 'www.httpbin.org', 'postman-echo.com'];
const psl = [];
for (const engine of ['chromium', 'firefox', 'webkit']) {
  const browser = await open(engine);
  try {
    for (const host of HOSTS) {
      const context = await browser.newContext();
      const page = await context.newPage();
      const url = `https://${host}`;
      await context.addCookies([{ name: 'via_add_cookies', value: '1', url }]);
      await json(page, `${url}/cookies/set?via_set_cookie=1`); // Set-Cookie, then a redirect to /cookies
      await page.evaluate(() => { document.cookie = 'via_document_cookie=1; path=/; max-age=3600'; });
      const kept = names(await context.cookies(url));
      psl.push({
        engine,
        host,
        add_cookies: kept.includes('via_add_cookies'),
        set_cookie: kept.includes('via_set_cookie'),
        document_cookie: kept.includes('via_document_cookie'),
        server_sees: Object.keys((await json(page, `${url}/cookies`)).cookies).sort(),
      });
      await context.close();
    }
  } finally {
    await browser.close();
  }
}

console.log(JSON.stringify({ jar, psl }, null, 2));
