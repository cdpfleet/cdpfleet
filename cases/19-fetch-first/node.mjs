// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, request } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = new URL(process.env.PROXY_URL);

// Three pages, one question each: is the data in the HTML, or does it need JavaScript?
const TARGETS = [
  { url: 'https://books.toscrape.com/', item: 'product_pod', expected: 20 },
  { url: 'https://quotes.toscrape.com/js/', item: 'quote', expected: 10 },
  { url: 'https://quotes.toscrape.com/scroll', item: 'quote', expected: 10 },
];

// Step 1: a plain HTTP GET through the same proxy, no browser (Playwright's request API
// runs locally; the proxy keeps the exit IP identical to the browser's).
const http = await request.newContext({
  proxy: { server: `${PROXY.protocol}//${PROXY.host}`, username: decodeURIComponent(PROXY.username), password: decodeURIComponent(PROXY.password) },
  userAgent: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36',
});
const countInHtml = (html, cls) => (html.match(new RegExp(`class="[^"]*\\b${cls}\\b[^"]*"`, 'g')) || []).length;

const rows = [];
for (const t of TARGETS) {
  const t0 = Date.now();
  const res = await http.get(t.url, { timeout: 60000 });
  const html = await res.text();
  rows.push({ url: t.url, http_status: res.status(), html_kb: Math.round(html.length / 1024), items_in_html: countInHtml(html, t.item), fetch_seconds: (Date.now() - t0) / 1000 });
}
await http.dispose();

// Step 2: only the pages whose HTML didn't have the items get a browser.
const needsBrowser = rows.filter((r, i) => r.items_in_html < TARGETS[i].expected);
if (needsBrowser.length) {
  const launch = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
  });
  if (!launch.ok) throw new Error(`launch ${launch.status} ${await launch.text()}`);
  const { wsUrl } = await launch.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    for (const r of needsBrowser) {
      const t = TARGETS.find((x) => x.url === r.url);
      const page = await browser.newPage();
      const t0 = Date.now();
      await page.goto(r.url, { timeout: 60000 });
      await page.locator(`.${t.item}`).first().waitFor({ timeout: 60000 });
      r.items_in_browser = await page.locator(`.${t.item}`).count();
      r.browser_seconds = (Date.now() - t0) / 1000;
      await page.close();
    }
  } finally {
    await browser.close();
  }
}
for (const r of rows) {
  r.needs_browser = r.items_in_browser !== undefined;
  r.items_in_browser ??= null;
  r.browser_seconds ??= null;
}
console.log(JSON.stringify(rows, null, 2));
