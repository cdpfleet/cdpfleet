// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SETUPS = [
  { label: 'Chrome', engine: 'chrome', type: chromium, api: 'typeof chrome !== "undefined" && !!chrome.runtime' },
  { label: 'Firefox', engine: 'firefox', type: firefox, api: 'typeof browser !== "undefined" && !!browser.runtime' },
];
const SITE = 'https://books.toscrape.com/';

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(engine, body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return { status: res.status, ...(await res.json()) };
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${engine} ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

// What an extension's content script does: runs in every page before the page's own scripts.
const CONTENT_SCRIPT = `
  window.__myExtension = true;
  document.addEventListener('DOMContentLoaded', () => {
    const b = document.createElement('div');
    b.id = 'my-extension-banner';
    b.textContent = 'Injected on ' + location.pathname;
    document.body.prepend(b);
  });`;

// Try to load an extension the way a local launch would: Chromium flags and an "extensions" list.
// The fleet refuses browser-process fields with 400 unsupported_option, so nothing is started.
async function tryExtensions(engine) {
  const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({
      proxy: process.env.PROXY_URL,
      args: ['--load-extension=/tmp/my-extension', '--user-agent=ARGS-WERE-APPLIED'],
      extensions: ['/tmp/my-extension'],
    }),
  });
  const body = await res.json().catch(() => ({}));
  return { status: res.status, error: body.error ?? null };
}

async function one(s) {
  const refused = await tryExtensions(s.engine);
  const out = { label: s.label, launch_status: refused.status, launch_error: refused.error };
  // A normal launch, for the Playwright equivalents.
  const session = await launch(s.engine, { proxy: process.env.PROXY_URL });
  const browser = await s.type.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    // chrome.runtime / browser.runtime exist in a page only when an extension talks to it.
    out.extension_api = await page.evaluate(s.api);
    await page.close();

    // The Playwright equivalents, in one context.
    const context = await browser.newContext();
    await context.addInitScript(CONTENT_SCRIPT); // content script
    await context.setExtraHTTPHeaders({ 'x-my-extension': 'on' }); // header rewriting
    let blocked = 0; // request blocking, like an ad or image blocker
    await context.route(/\.(png|jpe?g|gif|webp)(\?|$)/, (route) => { blocked++; return route.abort(); });
    const p = await context.newPage();
    let injected = 0;
    for (const url of [SITE, `${SITE}catalogue/page-2.html`]) {
      await p.goto(url, { timeout: 60000 });
      if (await p.locator('#my-extension-banner').count()) injected++;
    }
    out.content_script_pages = injected;
    out.blocked_images = blocked;
    // httpbin echoes the request headers; read the response body, not the rendered page.
    const { headers } = await (await p.goto('https://httpbin.org/headers', { timeout: 60000 })).json();
    out.header_added = headers['X-My-Extension'] === 'on';
  } finally {
    await browser.close();
  }
  return out;
}

const results = [];
for (const s of SETUPS) results.push(await one(s));
console.log(JSON.stringify({ results }, null, 2));
