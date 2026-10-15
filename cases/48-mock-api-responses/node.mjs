// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const TYPES = { chromium, firefox, webkit };
const SITE = 'https://shop.example.test'; // doesn't exist: every request to it is answered by the script

// A tiny shop front: loads products from its API, shows an error state, and a slideshow title
// from a real public API (httpbin.org/json).
const HTML = `<html><head><meta charset="utf-8"></head><body><h1>Shop</h1><ul id="list"></ul><p id="status"></p><p id="title"></p>
<img src="${SITE}/banner.png"><img src="https://httpbin.org/image/png">
<script>
  fetch('/api/products').then((r) => r.ok ? r.json() : Promise.reject(r.status))
    .then((items) => { for (const p of items) document.getElementById('list').insertAdjacentHTML('beforeend', '<li>' + p.name + ' — ' + p.price + '</li>'); document.getElementById('status').textContent = 'ok'; })
    .catch((code) => { document.getElementById('status').textContent = 'error ' + code; });
  fetch('https://httpbin.org/json').then((r) => r.json()).then((j) => { document.getElementById('title').textContent = j.slideshow.title; });
</script></body></html>`;

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

// Open the shop with the API answering `api` ('stub' or 'error'); returns what the page shows.
async function visit(context, api) {
  const page = await context.newPage();
  let aborted = 0;
  // The page itself and its API never leave the browser (the page also declares its charset).
  await page.route(`${SITE}/`, (route) => route.fulfill({ contentType: 'text/html; charset=utf-8', body: HTML }));
  await page.route(`${SITE}/api/products`, (route) => (api === 'stub'
    ? route.fulfill({ json: [{ name: 'Red mug', price: '$9' }, { name: 'Blue mug', price: '$11' }, { name: 'Teapot', price: '$24' }] })
    : route.fulfill({ status: 500, body: 'boom' })));
  // A real response, changed on its way in.
  await page.route('https://httpbin.org/json', async (route) => {
    const response = await route.fetch();
    const json = await response.json();
    json.slideshow.title = `Patched: ${json.slideshow.title}`;
    await route.fulfill({ response, json });
  });
  // Images are dropped before they're requested.
  await page.route(/\.png$|\/image\//, (route) => { aborted++; return route.abort(); });
  await page.goto(`${SITE}/`);
  await page.waitForFunction(() => document.getElementById('status').textContent && document.getElementById('title').textContent, null, { timeout: 30000 });
  const out = await page.evaluate(() => ({
    items: [...document.querySelectorAll('#list li')].map((li) => li.textContent),
    status: document.getElementById('status').textContent,
    title: document.getElementById('title').textContent,
  }));
  await page.close();
  return { ...out, images_aborted: aborted };
}

// Does the charset in the fulfilled Content-Type alone decode non-ASCII text? (No <meta>.)
async function charsetHeaderOnly(context) {
  const page = await context.newPage();
  await page.route(`${SITE}/plain`, (route) => route.fulfill({ contentType: 'text/html; charset=utf-8', body: '<p>Red mug — $9</p>' }));
  await page.goto(`${SITE}/plain`);
  const text = await page.textContent('p');
  await page.close();
  return text;
}

async function one(engine) {
  const { wsUrl } = await launch(engine, { proxy: process.env.PROXY_URL, headless: true, record: false });
  const browser = await TYPES[engine].connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const context = await browser.newContext();
    const stub = await visit(context, 'stub');
    const error = await visit(context, 'error');
    const headerOnly = await charsetHeaderOnly(context);
    return { engine, items: stub.items, status: stub.status, title: stub.title, images_aborted: stub.images_aborted, error_status: error.status, header_only_text: headerOnly };
  } finally {
    await browser.close();
  }
}

const results = [];
for (const engine of ['chromium', 'firefox', 'webkit']) results.push(await one(engine));
console.log(JSON.stringify({ results }, null, 2));
