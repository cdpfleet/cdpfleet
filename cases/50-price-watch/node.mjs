// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const WAIT_SECONDS = 120; // between the two snapshots — in a real watcher, your scheduler's interval

// What to watch: a catalogue page (its price only changes when the shop changes it) and a live
// market price that moves by the second.
const TARGETS = [
  { name: 'Book price', url: 'https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html',
    read: () => ({ price: document.querySelector('.product_main .price_color').textContent.trim(), stock: document.querySelector('.product_main .availability').textContent.trim().replace(/\s+/g, ' ') }) },
  { name: 'Bitcoin spot (USD)', url: 'https://api.coinbase.com/v2/prices/BTC-USD/spot',
    read: () => ({ price: JSON.parse(document.body.innerText).data.amount }) },
];

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

// One snapshot = one short session: launch, read every target, close. Nothing is billed while
// waiting, and an idle session would end after 2 minutes (the default) anyway.
async function snapshot() {
  const t = Date.now();
  const { wsUrl } = await launch({ proxy: process.env.PROXY_URL, headless: true, record: false });
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    const values = {};
    for (const target of TARGETS) {
      for (let attempt = 1; ; attempt++) { // proxies drop a tunnel now and then
        try {
          await page.goto(target.url, { timeout: 45000 });
          values[target.name] = await page.evaluate(target.read);
          break;
        } catch (err) {
          if (attempt === 3) throw err;
        }
      }
    }
    return { at: new Date().toISOString(), seconds: Math.round((Date.now() - t) / 100) / 10, values };
  } finally {
    await browser.close();
  }
}

// Field-by-field diff of two snapshots, with the percentage for numeric prices.
function diff(a, b) {
  return TARGETS.map(({ name }) => {
    const changes = Object.keys(a.values[name]).filter((k) => a.values[name][k] !== b.values[name][k]).map((k) => {
      const [x, y] = [a.values[name][k], b.values[name][k]];
      const [nx, ny] = [x, y].map((v) => Number(String(v).replace(/[^0-9.]/g, '')));
      return { field: k, before: x, after: y, ...(nx && ny ? { percent: Math.round(((ny - nx) / nx) * 1e4) / 100 } : {}) };
    });
    return { target: name, changed: changes.length > 0, changes, now: b.values[name] };
  });
}

const first = await snapshot();
await new Promise((r) => setTimeout(r, WAIT_SECONDS * 1000));
const second = await snapshot();
console.log(JSON.stringify({
  wait_seconds: WAIT_SECONDS,
  billed_seconds: Math.round((first.seconds + second.seconds) * 10) / 10,
  first_at: first.at, second_at: second.at,
  diff: diff(first, second),
}, null, 2));
