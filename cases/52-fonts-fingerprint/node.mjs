// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { createHash } from 'node:crypto';
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// The families a font-fingerprinting script typically tries: Windows, macOS, Linux and popular extras.
const CANDIDATES = [
  'Arial', 'Arial Black', 'Calibri', 'Cambria', 'Candara', 'Comic Sans MS', 'Consolas', 'Constantia', 'Corbel', 'Courier New',
  'Georgia', 'Impact', 'Lucida Console', 'Lucida Sans Unicode', 'Microsoft Sans Serif', 'Palatino Linotype', 'Segoe UI', 'Tahoma',
  'Times New Roman', 'Trebuchet MS', 'Verdana', 'American Typewriter', 'Avenir', 'Avenir Next', 'Futura', 'Geneva', 'Gill Sans',
  'Helvetica', 'Helvetica Neue', 'Lucida Grande', 'Menlo', 'Monaco', 'Optima', 'SF Pro', 'DejaVu Sans', 'DejaVu Serif',
  'Liberation Sans', 'Liberation Mono', 'Noto Sans', 'Ubuntu', 'Cantarell', 'Roboto', 'Open Sans', 'Fira Code', 'JetBrains Mono',
  'Source Code Pro',
];

// The classic width probe: a family is "installed" if text set in it, with a generic fallback,
// measures differently from the generic family alone.
const PROBE = `(fonts) => {
  const span = document.createElement('span');
  span.textContent = 'mmmmmmmmmmlli10OWQ@#';
  span.style.cssText = 'position:absolute;left:-9999px;font-size:72px';
  document.body.appendChild(span);
  const size = (family) => { span.style.fontFamily = family; return span.offsetWidth + 'x' + span.offsetHeight; };
  const generics = ['monospace', 'serif', 'sans-serif'];
  const base = {};
  for (const g of generics) base[g] = size(g);
  const found = fonts.filter((f) => generics.some((g) => size('"' + f + '", ' + g) !== base[g]));
  span.remove();
  return { platform: navigator.platform, found, generic_sizes: generics.map((g) => base[g]) };
}`;

const SETUPS = [
  { setup: 'os: windows (launch 1)', body: { os: 'windows' } },
  { setup: 'os: windows (launch 2)', body: { os: 'windows' } },
  { setup: 'os: macos', body: { os: 'macos' } },
  { setup: 'os: linux', body: { os: 'linux' } },
  { setup: 'windows + fonts', body: { os: 'windows', fonts: ['Menlo', 'Helvetica Neue', 'Fira Code'] } },
  { setup: 'windows + fonts + custom_fonts_only', body: { os: 'windows', fonts: ['Calibri', 'Consolas', 'Segoe UI', 'Fira Code'], custom_fonts_only: true } },
];

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

const results = [];
for (const s of SETUPS) {
  const session = await launch({ ...s.body, proxy: process.env.PROXY_URL, headless: true, record: false });
  const browser = await firefox.connect(session.wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await (await browser.newContext()).newPage();
    await go(page, 'https://httpbin.org/html');
    const r = await page.evaluate(`(${PROBE})(${JSON.stringify(CANDIDATES)})`);
    results.push({
      setup: s.setup,
      platform: r.platform,
      detected: r.found.length,
      font_hash: createHash('sha256').update(r.found.join(',')).digest('hex').slice(0, 10),
      generics_identical: new Set(r.generic_sizes).size === 1,
      fonts: r.found,
    });
  } finally {
    await browser.close();
  }
}
console.log(JSON.stringify(results, null, 2));
