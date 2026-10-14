// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const TYPES = { chromium, firefox, webkit };

// Which browsers can print a page to PDF over a remote connection, headless and headful.
const SETUPS = [
  { label: 'Chromium, headless', engine: 'chromium', type: 'chromium', headless: true },
  { label: 'Chrome, headless', engine: 'chrome', type: 'chromium', headless: 'new' },
  { label: 'Chrome, headful', engine: 'chrome', type: 'chromium', headless: false },
  { label: 'Edge, headful', engine: 'edge', type: 'chromium', headless: false },
  { label: 'Firefox, headful', engine: 'firefox', type: 'firefox', headless: false },
  { label: 'Camoufox, headful', engine: 'camoufox', type: 'firefox', headless: false },
  { label: 'WebKit, headful', engine: 'webkit', type: 'webkit', headless: false },
];

// A page with a coloured background, a screen-only banner hidden by print CSS and enough text
// for several printed pages. setContent: no proxy traffic, the same page everywhere.
const HTML = `<html><head><style>
  body { font: 14px sans-serif; background: #e8f0ff; }
  .screen-only { background: #c00; color: #fff; padding: 8px; }
  @media print { .screen-only { display: none; } }
</style></head><body>
  <div class="screen-only">Cookie banner (screen only)</div>
  <h1>Quarterly report</h1>
  ${Array.from({ length: 120 }, (_, i) => `<p>Paragraph ${i + 1}: lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>`).join('')}
</body></html>`;

const pagesIn = (buf) => (buf.toString('latin1').match(/\/Type\s*\/Page(?!s)/g) || []).length;

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async function launch(engine, body) {
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return res.json();
    if (res.status !== 503 || attempt === 5) throw new Error(`launch ${engine} ${res.status} ${await res.text()}`);
    await new Promise((r) => setTimeout(r, (Number(res.headers.get('retry-after')) || 5) * 1000));
  }
}

async function one(s) {
  const { wsUrl } = await launch(s.engine, { proxy: process.env.PROXY_URL, headless: s.headless });
  const browser = await TYPES[s.type].connect(wsUrl, { headers: { 'x-api-key': KEY } });
  const out = { label: s.label };
  try {
    const page = await browser.newPage();
    await page.setContent(HTML);
    const t = Date.now();
    try {
      const pdf = await page.pdf({ format: 'A4' });
      const withBg = await page.pdf({ format: 'A4', printBackground: true });
      out.pdf = 'yes';
      out.pdf_bytes = pdf.length;
      out.pdf_pages = pagesIn(pdf);
      out.with_background_bytes = withBg.length;
      out.pdf_seconds = Math.round((Date.now() - t) / 10) / 100;
    } catch (err) {
      out.pdf = 'no';
      out.error = err.message.split('\n')[0].slice(0, 120);
    }
    // The fallback that works everywhere: print CSS + a full-page screenshot.
    await page.emulateMedia({ media: 'print' });
    out.print_css_applied = await page.evaluate(() => getComputedStyle(document.querySelector('.screen-only')).display === 'none');
    out.print_screenshot_kb = Math.round((await page.screenshot({ fullPage: true })).length / 1024);
  } finally {
    await browser.close();
  }
  return out;
}

const results = [];
for (const s of SETUPS) results.push(await one(s));
console.log(JSON.stringify({ results }, null, 2));
