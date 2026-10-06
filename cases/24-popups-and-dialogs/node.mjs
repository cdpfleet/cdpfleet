// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// A page with everything that interrupts a script: a new-tab link, window.open, and the
// three blocking dialogs. Served from the browser itself, so the case needs no third party.
const HTML = `<!doctype html><title>Interruptions</title>
<a id="blank" href="https://example.com/" target="_blank">open in a new tab</a>
<button id="open" onclick="window.open('https://example.com/?popup', 'pop', 'width=480,height=320')">window.open</button>
<button id="alert" onclick="alert('Saved!')">alert</button>
<button id="confirm" onclick="document.body.dataset.confirm = String(confirm('Delete 3 items?'))">confirm</button>
<button id="prompt" onclick="document.body.dataset.prompt = String(prompt('Your name?', 'anonymous'))">prompt</button>`;

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

try {
  const context = await browser.newContext();
  const page = await context.newPage();
  await page.setContent(HTML);
  const out = [];

  // New pages: listen on the context BEFORE the click, then wait for the popup to load.
  for (const [label, selector] of [['link with target=_blank', '#blank'], ['window.open()', '#open']]) {
    const [popup] = await Promise.all([context.waitForEvent('page'), page.click(selector)]);
    await popup.waitForLoadState('load', { timeout: 60000 });
    out.push({ event: label, what_happened: `new page: ${popup.url()} — "${await popup.title()}"`, handled_with: 'context.waitForEvent("page") + popup.waitForLoadState()', pages_open: context.pages().length, opener_is_main_page: (await popup.opener()) === page });
    await popup.close();
  }

  // Dialogs: without a handler Playwright dismisses them (confirm → false, prompt → null).
  await page.click('#confirm');
  out.push({ event: 'confirm() with no dialog handler', what_happened: `page saw confirm() return ${await page.evaluate(() => document.body.dataset.confirm)}`, handled_with: 'nothing — auto-dismissed', pages_open: context.pages().length, opener_is_main_page: null });

  // With a handler you decide: accept, dismiss, or type an answer.
  const seen = [];
  page.on('dialog', async (d) => {
    seen.push(`${d.type()}: "${d.message()}"`);
    if (d.type() === 'prompt') await d.accept('Ada Lovelace');
    else await d.accept();
  });
  await page.click('#alert');
  await page.click('#confirm');
  await page.click('#prompt');
  const results = await page.evaluate(() => ({ confirm: document.body.dataset.confirm, prompt: document.body.dataset.prompt }));
  out.push({ event: 'alert()', what_happened: seen[0], handled_with: 'dialog.accept()', pages_open: context.pages().length, opener_is_main_page: null });
  out.push({ event: 'confirm() with a handler', what_happened: `${seen[1]} → page saw ${results.confirm}`, handled_with: 'dialog.accept()', pages_open: context.pages().length, opener_is_main_page: null });
  out.push({ event: 'prompt()', what_happened: `${seen[2]} → page saw "${results.prompt}"`, handled_with: 'dialog.accept("Ada Lovelace")', pages_open: context.pages().length, opener_is_main_page: null });

  console.log(JSON.stringify(out, null, 2));
} finally {
  await browser.close();
}
