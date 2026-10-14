// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox, webkit } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const TYPES = { chromium, firefox, webkit };
const SETUPS = [
  { label: 'Chromium, headless', engine: 'chromium', type: 'chromium', headless: true },
  { label: 'Chrome, headful', engine: 'chrome', type: 'chromium', headless: false },
  { label: 'Firefox, headful', engine: 'firefox', type: 'firefox', headless: false },
  { label: 'WebKit, headful', engine: 'webkit', type: 'webkit', headless: false },
];

// A made-up https origin served by Playwright's router: a secure context (the clipboard and
// file APIs need one) with no network traffic. The page has a file input and a button that
// opens the File System Access picker; both report what they received in data attributes.
const PAGE = 'https://app.example.test/';
const HTML = `<html><body>
<input type="file" id="f" onchange="document.body.dataset.input = this.files[0].name + ':' + this.files[0].size">
<button id="b" onclick="showOpenFilePicker().then(async ([h]) => { const f = await h.getFile(); document.body.dataset.picked = f.name + ':' + f.size; }).catch((e) => { document.body.dataset.picked = 'error: ' + e.name; })">open</button>
</body></html>`;
const serve = (context) => context.route(`${PAGE}**`, (route) => route.fulfill({ contentType: 'text/html', body: HTML }));

// Clipboard round trip inside the page; a permission prompt nobody answers would hang, so 3 s cap.
const CLIPBOARD = `(async () => {
  const cap = (p) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('timed out (prompt?)')), 3000))]);
  const out = {};
  try { await cap(navigator.clipboard.writeText('copied-' + Date.now())); out.write = 'ok'; } catch (e) { out.write = e.name === 'Error' ? e.message : e.name; }
  try { const t = await cap(navigator.clipboard.readText()); out.read = t.startsWith('copied-') ? 'ok' : 'empty'; } catch (e) { out.read = e.name === 'Error' ? e.message : e.name; }
  return out;
})()`;

// The origin-private file system: a sandboxed folder per site, no picker, no permission.
const OPFS = `(async () => {
  try {
    const root = await navigator.storage.getDirectory();
    const fh = await root.getFileHandle('note.txt', { create: true });
    const w = await fh.createWritable(); await w.write('hello opfs'); await w.close();
    return (await (await fh.getFile()).text()) === 'hello opfs' ? 'ok' : 'mismatch';
  } catch (e) { return 'no (' + e.name + ')'; }
})()`;

// Stands in for the native picker: returns a handle to a file the script provides.
const PICKER_STUB = `window.showOpenFilePicker = async () => [{
  kind: 'file', name: 'report.csv', getFile: async () => new File(['id,total\\n1,42\\n'], 'report.csv', { type: 'text/csv' }),
}];`;

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

async function freshPage(browser, initScript) {
  const context = await browser.newContext();
  await serve(context);
  if (initScript) await context.addInitScript(initScript);
  const page = await context.newPage();
  await page.goto(PAGE);
  return { context, page };
}

async function one(s) {
  const { wsUrl } = await launch(s.engine, { proxy: process.env.PROXY_URL, headless: s.headless });
  const browser = await TYPES[s.type].connect(wsUrl, { headers: { 'x-api-key': KEY } });
  const out = { label: s.label };
  try {
    // 1. Clipboard with no permissions granted.
    let { context, page } = await freshPage(browser);
    const def = await page.evaluate(CLIPBOARD);
    out.clipboard_write_default = def.write;
    out.clipboard_read_default = def.read;

    // 2. Files in the same page: <input type=file>, the File System Access picker, the private file system.
    const chooser = page.waitForEvent('filechooser', { timeout: 3000 }).catch(() => null);
    await page.click('#f');
    const fc = await chooser;
    if (fc) await fc.setFiles({ name: 'report.csv', mimeType: 'text/csv', buffer: Buffer.from('id,total\n1,42\n') });
    out.input_set_files = fc ? await page.evaluate(() => document.body.dataset.input || 'nothing') : 'no filechooser event';
    out.show_open_file_picker = await page.evaluate(() => typeof window.showOpenFilePicker === 'function');
    if (out.show_open_file_picker) {
      const ev = page.waitForEvent('filechooser', { timeout: 3000 }).then(() => true, () => false);
      await page.click('#b');
      out.picker_filechooser_event = await ev;
    } else {
      out.picker_filechooser_event = false;
    }
    out.private_file_system = await page.evaluate(OPFS);
    await context.close();

    // 3. The picker stubbed with an init script: the page's own code receives our file.
    ({ context, page } = await freshPage(browser, PICKER_STUB));
    await page.click('#b');
    await page.waitForFunction(() => document.body.dataset.picked, null, { timeout: 5000 });
    out.picker_stubbed = await page.evaluate(() => document.body.dataset.picked);
    await context.close();

    // 4. Clipboard with Chromium's permission names granted. Some engines reject the names —
    // at grantPermissions or when the next page opens.
    context = await browser.newContext();
    await serve(context);
    try {
      await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: PAGE });
      page = await context.newPage();
      await page.goto(PAGE);
      out.grant_clipboard = 'accepted';
      const granted = await page.evaluate(CLIPBOARD);
      out.clipboard_write_granted = granted.write;
      out.clipboard_read_granted = granted.read;
    } catch (err) {
      out.grant_clipboard = err.message.split('\n')[0].replace(/^.*?: /, '').slice(0, 80);
    }
  } finally {
    await browser.close();
  }
  return out;
}

const results = [];
for (const s of SETUPS) results.push(await one(s));
console.log(JSON.stringify({ results }, null, 2));
