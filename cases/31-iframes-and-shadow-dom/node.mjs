// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// Content a plain document.querySelector can't see: a same-origin iframe, a cross-origin
// iframe, an open shadow root (with another one nested inside) and a closed shadow root.
const HTML = `<!doctype html><title>Hidden content</title>
<h1>Main document</h1>
<iframe id="same" srcdoc="<p id='inner'>same-origin iframe text</p>"></iframe>
<iframe id="cross" src="https://httpbin.org/html"></iframe>
<open-card></open-card>
<closed-card></closed-card>
<script>
customElements.define('open-card', class extends HTMLElement {
  connectedCallback() {
    const root = this.attachShadow({ mode: 'open' });
    root.innerHTML = '<p class="msg">open shadow text</p><nested-badge></nested-badge>';
  }
});
customElements.define('nested-badge', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<span class="badge">nested shadow text</span>'; }
});
customElements.define('closed-card', class extends HTMLElement {
  connectedCallback() { this.attachShadow({ mode: 'closed' }).innerHTML = '<p class="secret">closed shadow text</p>'; }
});
</script>`;

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

try {
  const page = await browser.newPage();
  await page.setContent(HTML);
  await page.frameLocator('#cross').locator('h1').waitFor({ timeout: 60000 });

  const qs = (sel) => page.evaluate((s) => document.querySelector(s)?.textContent ?? null, sel);
  const pw = async (loc) => (await loc.count()) ? (await loc.first().textContent()) : null;

  const rows = [
    { target: 'same-origin iframe', querySelector: await qs('#inner'), playwright: await pw(page.frameLocator('#same').locator('#inner')), how: "page.frameLocator('#same').locator('#inner')" },
    { target: 'cross-origin iframe', querySelector: await qs('h1 + div p'), playwright: await pw(page.frameLocator('#cross').locator('h1')), how: "page.frameLocator('#cross').locator('h1')" },
    { target: 'open shadow root', querySelector: await qs('.msg'), playwright: await pw(page.locator('.msg')), how: "page.locator('.msg') — CSS pierces open shadow roots" },
    { target: 'nested open shadow root', querySelector: await qs('.badge'), playwright: await pw(page.locator('.badge')), how: "page.locator('.badge') — any depth" },
    { target: 'closed shadow root', querySelector: await qs('.secret'), playwright: await pw(page.locator('.secret')), how: 'not reachable from page scripts or locators' },
  ];
  // The cross-origin frame is a separate document: its URL and title come from the frame object.
  const cross = page.frames().find((f) => f.url().startsWith('https://httpbin.org'));
  console.log(JSON.stringify({ frames: page.frames().length, cross_origin_frame: { url: cross?.url() ?? null, title: cross ? await cross.title() : null }, rows }, null, 2));
} finally {
  await browser.close();
}
