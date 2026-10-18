// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITE = 'https://practicesoftwaretesting.com';  // Toolshop: a public demo shop built for test automation
const API = 'https://api.practicesoftwaretesting.com';
const LOGIN = { email: 'customer@practicesoftwaretesting.com', password: 'welcome01' }; // the site's published demo account

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

const { wsUrl } = await launch({ proxy: process.env.PROXY_URL, headless: 'new', record: false });
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const page = await browser.newPage();
  // The app can take 30 s to render through a rotating residential exit: wait up to 90 s.
  page.setDefaultTimeout(90000);

  // 1. Log in like a person; the site stores its token and sends it with every API call.
  await page.goto(`${SITE}/auth/login`);
  await page.fill('#email', LOGIN.email);
  await page.fill('#password', LOGIN.password);
  await page.click('[type=submit]');
  await page.waitForURL('**/account');

  // 2. Find the token twice: in localStorage, and in the header of the page's own API request.
  const stored = await page.evaluate(() => localStorage.getItem('auth-token'));
  let uiCalls = 0;
  const countCall = (r) => { if (r.url().startsWith(`${API}/invoices`)) uiCalls++; };
  page.on('request', countCall);
  const [firstCall] = await Promise.all([
    page.waitForRequest((r) => r.url().startsWith(`${API}/invoices`)),
    page.goto(`${SITE}/account/invoices`),
  ]);
  const header = (await firstCall.allHeaders()).authorization || '';
  const claims = JSON.parse(Buffer.from(stored.split('.')[1], 'base64url').toString());

  // 3. The page's way: click through the pager, reading the table each time.
  await page.waitForSelector('table tbody tr');
  const t0 = Date.now();
  const viaUi = [];
  const readTable = () => page.$$eval('table tbody tr td:first-child', (tds) => tds.map((td) => td.textContent.trim()));
  viaUi.push(...await readTable());
  const lastPage = Number(await page.locator('button[aria-label^="Page-"]').last().innerText());
  for (let n = 2; n <= lastPage; n++) {
    await Promise.all([
      page.waitForResponse((r) => r.url() === `${API}/invoices?page=${n}`),
      page.click('[data-test="pagination-next"]'),
    ]);
    await page.waitForFunction((first) => document.querySelector('table tbody tr td')?.textContent.trim() !== first, viaUi[viaUi.length - 15]);
    viaUi.push(...await readTable());
  }
  const uiSeconds = (Date.now() - t0) / 1000;
  page.off('request', countCall);
  const columns = await page.locator('table thead th').count();

  // 4. The API's way: the same token on fetch() from inside the page — the browser makes the
  //    requests, through the session's proxy. Page 1 tells last_page; then all pages one by one,
  //    and all pages at once.
  const viaApi = await page.evaluate(async ({ api, token }) => {
    const get = (n) => fetch(`${api}/invoices?page=${n}`, { headers: { authorization: `Bearer ${token}` } }).then(async (r) => ({ status: r.status, text: await r.text() }));
    const meta = JSON.parse((await get(1)).text);
    const numbers = Array.from({ length: meta.last_page }, (_, i) => i + 1);
    let t = performance.now();
    const pages = [];
    for (const n of numbers) pages.push(await get(n));
    const sequential = (performance.now() - t) / 1000;
    t = performance.now();
    await Promise.all(numbers.map(get));
    const parallel = (performance.now() - t) / 1000;
    const noToken = await fetch(`${api}/invoices?page=1`).then((r) => r.status);
    const items = pages.flatMap((p) => JSON.parse(p.text).data);
    return {
      sequential, parallel, total: meta.total, perPage: meta.per_page, lastPage: meta.last_page,
      requests: pages.length, bytes: pages.reduce((s, p) => s + p.text.length, 0), statuses: [...new Set(pages.map((p) => p.status))],
      invoices: items.map((i) => i.invoice_number), fields: Object.keys(items[0]).length, noToken,
    };
  }, { api: API, token: stored });

  const sameSet = viaUi.length === viaApi.invoices.length && [...viaUi].sort().join() === [...viaApi.invoices].sort().join();
  console.log(JSON.stringify({
    token: {
      in_local_storage: stored ? 'auth-token' : null,
      header_matches: header === `Bearer ${stored}`,
      lifetime_seconds: claims.exp - claims.iat,
      seconds_left_at_end: claims.exp - Math.floor(Date.now() / 1000),
    },
    api: { total: viaApi.total, per_page: viaApi.perPage, last_page: viaApi.lastPage, without_token: viaApi.noToken },
    ui: { pages: lastPage, invoices: viaUi.length, invoice_calls: uiCalls, columns, seconds: uiSeconds },
    direct: { requests: viaApi.requests, statuses: viaApi.statuses, invoices: viaApi.invoices.length, fields_per_invoice: viaApi.fields, json_bytes: viaApi.bytes, seconds_one_by_one: viaApi.sequential, seconds_all_at_once: viaApi.parallel },
    same_invoices: sameSet,
    ui_missing_from_api: [...new Set(viaUi)].filter((n) => !viaApi.invoices.includes(n)).length,
  }));
} finally {
  await browser.close();
}
