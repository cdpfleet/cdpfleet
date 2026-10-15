// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
import { createHash, randomBytes } from 'node:crypto';
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const NAME = `case49-node-${randomBytes(4).toString('hex')}`; // one profile per run
const SITE = 'https://httpbin.org/html';
// One country for every launch: Camoufox matches timezone and locale to the exit IP.
const BODY = { proxy: process.env.PROXY_URL_US, headless: true, os: 'windows', record: false };

async function api(method, path) {
  const res = await fetch(`https://cdpfleet.com${path}`, { method, headers: { 'x-api-key': KEY } });
  return { status: res.status, body: await res.json().catch(() => ({})) };
}
// The node saves the profile after the session's browser has exited: wait for the new version.
async function waitForVersion(version) {
  for (let i = 0; i < 60; i++) {
    const p = (await api('GET', '/v1/me/profiles')).body.profiles?.find((x) => x.name === NAME);
    if (p?.version >= version && !p.in_use) return p;
    await new Promise((r) => setTimeout(r, 2000));
  }
  throw new Error(`profile ${NAME} never reached version ${version}`);
}

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch('https://starter.cdpfleet.com/camoufox/session', {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return res.json();
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// Proxies drop a tunnel now and then: up to two retries.
async function go(page, url) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await page.goto(url, { timeout: 60000 });
    } catch (err) {
      if (attempt === 3) throw err;
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

// What a fingerprinting script reads, plus the "login" kept in a cookie and localStorage.
async function visit(page, { logIn }) {
  await go(page, SITE);
  if (logIn) await page.evaluate(() => { document.cookie = 'session=alice-7f3; max-age=604800; path=/'; localStorage.setItem('user', 'alice'); });
  const fp = await page.evaluate(() => {
    const gl = document.createElement('canvas').getContext('webgl');
    const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
    const draw = () => {
      const c = document.createElement('canvas');
      c.width = 240; c.height = 60;
      const ctx = c.getContext('2d');
      ctx.font = '18px Arial'; ctx.fillStyle = '#f60'; ctx.fillRect(10, 10, 100, 30); ctx.fillStyle = '#069'; ctx.fillText('cdpfleet device 49', 4, 40);
      return c.toDataURL();
    };
    return {
      user_agent: navigator.userAgent,
      platform: navigator.platform,
      cores: navigator.hardwareConcurrency,
      screen: `${screen.width}x${screen.height}`,
      webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
      canvas: draw(),
      canvas_again: draw(), // the same drawing twice in one session
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      languages: navigator.languages,
      login_cookie: document.cookie.includes('session=alice-7f3'),
      login_storage: localStorage.getItem('user') === 'alice',
    };
  });
  const hash = (x) => createHash('sha256').update(x).digest('hex').slice(0, 12);
  fp.canvas_same_in_session = fp.canvas === fp.canvas_again;
  fp.canvas = hash(fp.canvas);
  delete fp.canvas_again;
  return fp;
}

async function session(body, logIn) {
  const s = await launch(body);
  const browser = await firefox.connect(s.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    // With a profile, its context is browser.contexts()[0]; without one, a fresh context.
    const context = body.profile ? browser.contexts()[0] : await browser.newContext();
    const page = context.pages()[0] ?? await context.newPage();
    return await visit(page, { logIn });
  } finally {
    await browser.close();
  }
}

// The device's hardware-like values (user agent, platform, cores, screen, GPU); the canvas is
// reported on its own.
const device = (r) => createHash('sha256').update(JSON.stringify([r.user_agent, r.platform, r.cores, r.screen, r.webgl_renderer])).digest('hex').slice(0, 10);
try {
  // Four launches with one profile: the first creates it (device + login), the rest restore it.
  const withProfile = [];
  for (let run = 1; run <= 4; run++) {
    withProfile.push({ run, ...(await session({ ...BODY, profile: NAME }, run === 1)) });
    await waitForVersion(run);
  }
  // Two launches without a profile, for contrast.
  const without = [];
  for (let run = 1; run <= 2; run++) without.push({ run, ...(await session(BODY, false)) });
  const pick = (r) => ({ run: r.run, device: device(r), user_agent: r.user_agent, cores: r.cores, screen: r.screen, webgl_renderer: r.webgl_renderer, canvas: r.canvas, canvas_same_in_session: r.canvas_same_in_session, timezone: r.timezone, login_cookie: r.login_cookie, login_storage: r.login_storage });
  console.log(JSON.stringify({ with_profile: withProfile.map(pick), without_profile: without.map(pick) }, null, 2));
} finally {
  await api('DELETE', `/v1/me/profiles/${NAME}`);
}
