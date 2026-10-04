// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';
import { readFileSync, mkdtempSync } from 'node:fs';
import { inflateRawSync } from 'node:zlib';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const dir = mkdtempSync(path.join(tmpdir(), 'cdpfleet-replay-'));

// Reads a zip's central directory and inflates the entries we want to look into.
function zipEntries(buf) {
  let p = buf.length - 22;
  while (buf.readUInt32LE(p) !== 0x06054b50) p--;
  const total = buf.readUInt16LE(p + 10);
  let q = buf.readUInt32LE(p + 16);
  const entries = [];
  for (let i = 0; i < total; i++) {
    const method = buf.readUInt16LE(q + 10);
    const csize = buf.readUInt32LE(q + 20);
    const nameLen = buf.readUInt16LE(q + 28);
    const extraLen = buf.readUInt16LE(q + 30);
    const commentLen = buf.readUInt16LE(q + 32);
    const local = buf.readUInt32LE(q + 42);
    const name = buf.toString('utf8', q + 46, q + 46 + nameLen);
    const dataStart = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
    const raw = buf.subarray(dataStart, dataStart + csize);
    entries.push({ name, read: () => (method === 8 ? inflateRawSync(raw) : raw).toString('utf8') });
    q += 46 + nameLen + extraLen + commentLen;
  }
  return entries;
}

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
let video;
const t = Date.now();
try {
  // Video is recorded on the remote browser and fetched when the page closes; the trace is
  // assembled by the client from events and screenshots streamed over the same connection.
  const context = await browser.newContext({ recordVideo: { dir, size: { width: 1280, height: 720 } }, viewport: { width: 1280, height: 720 } });
  await context.tracing.start({ screenshots: true, snapshots: true });
  const page = await context.newPage();
  video = page.video();
  await page.goto('https://example.com/', { timeout: 60000 });
  await page.goto('https://httpbin.org/forms/post', { timeout: 60000 });
  await page.getByLabel('Customer name').fill('Ada Lovelace');
  await page.getByLabel('Large').check();
  await page.getByRole('button', { name: 'Submit order' }).click();
  await page.waitForURL('**/post', { timeout: 60000 }); // httpbin echoes the form as JSON
  await page.screenshot({ path: path.join(dir, 'final.png') });
  await context.tracing.stop({ path: path.join(dir, 'trace.zip') });
  await context.close(); // finishes the video
  await video.saveAs(path.join(dir, 'session.webm'));
} finally {
  await browser.close();
}
const seconds = (Date.now() - t) / 1000;

const trace = readFileSync(path.join(dir, 'trace.zip'));
const entries = zipEntries(trace);
const events = entries.filter((e) => /\.trace$/.test(e.name)).flatMap((e) => e.read().split('\n').filter(Boolean).map((l) => JSON.parse(l)));
const actions = events.filter((e) => e.type === 'before' && e.method).map((e) => e.method);
const network = entries.filter((e) => /\.network$/.test(e.name)).flatMap((e) => e.read().split('\n').filter(Boolean)).length;
const webm = readFileSync(path.join(dir, 'session.webm'));

console.log(JSON.stringify([
  { artifact: 'trace.zip', bytes: trace.length, detail: `${actions.length} actions: ${actions.join(', ')}`, screenshots: entries.filter((e) => /^resources\/.*\.jpeg$/.test(e.name)).length, snapshots: events.filter((e) => e.type === 'frame-snapshot').length, network_entries: network, open_with: 'npx playwright show-trace trace.zip' },
  { artifact: 'session.webm', bytes: webm.length, detail: webm.readUInt32BE(0) === 0x1a45dfa3 ? 'valid WebM (EBML header)' : 'not a WebM file', screenshots: null, snapshots: null, network_entries: null, open_with: 'any video player' },
  { artifact: 'final.png', bytes: readFileSync(path.join(dir, 'final.png')).length, detail: 'screenshot after the last step', screenshots: null, snapshots: null, network_entries: null, open_with: 'image viewer' },
  { artifact: 'whole run', bytes: null, detail: `${seconds.toFixed(1)} s including recording and downloads`, screenshots: null, snapshots: null, network_entries: null, open_with: null },
], null, 2));
