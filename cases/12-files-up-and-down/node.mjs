// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';
import { createHash, randomBytes } from 'node:crypto';
import { writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');

// A local file to upload: 2,000 CSV rows (~60 KB).
const uploadPath = path.join(tmpdir(), 'cdpfleet-upload.csv');
const rows = ['id,token'];
for (let i = 1; i <= 2000; i++) rows.push(`${i},${randomBytes(12).toString('hex')}`);
writeFileSync(uploadPath, `${rows.join('\n')}\n`);
const local = readFileSync(uploadPath);

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// A small page on httpbin.org's origin with an upload form and a download link.
const PAGE = `<form method="post" action="/anything" enctype="multipart/form-data">
  <input type="file" name="upload" id="file"><button id="send">Send</button></form>
<a id="data" href="/bytes/102400?seed=42" download="data.bin">data</a>`;

try {
  const page = await browser.newPage();
  await page.route('https://httpbin.org/files-demo', (route) => route.fulfill({ contentType: 'text/html', body: PAGE }));
  await page.goto('https://httpbin.org/files-demo', { timeout: 60000 });

  // Upload: setInputFiles reads the file HERE and streams it to the remote browser.
  let t = Date.now();
  await page.setInputFiles('#file', uploadPath);
  const [answer] = await Promise.all([page.waitForNavigation({ timeout: 60000 }), page.click('#send')]);
  const echoed = (await answer.json()).files.upload;
  const uploadMs = Date.now() - t;

  // Download: the file lands on the remote server; saveAs streams it back to this machine.
  await page.goto('https://httpbin.org/files-demo', { timeout: 60000 });
  t = Date.now();
  const [download] = await Promise.all([page.waitForEvent('download', { timeout: 60000 }), page.click('#data')]);
  const downloadPath = path.join(tmpdir(), download.suggestedFilename());
  await download.saveAs(downloadPath);
  const downloadMs = Date.now() - t;
  const got = readFileSync(downloadPath);
  // The same seeded bytes fetched directly from here, to prove the copy is exact.
  const direct = Buffer.from(await (await fetch('https://httpbin.org/bytes/102400?seed=42')).arrayBuffer());

  console.log(JSON.stringify({
    upload: {
      local_file: path.basename(uploadPath), bytes: local.length, sha256: sha256(local),
      server_received_bytes: Buffer.byteLength(echoed), server_sha256: sha256(Buffer.from(echoed)),
      identical: sha256(local) === sha256(Buffer.from(echoed)), ms: uploadMs,
    },
    download: {
      suggested_filename: download.suggestedFilename(), bytes: got.length, sha256: sha256(got),
      direct_sha256: sha256(direct), identical: sha256(got) === sha256(direct), ms: downloadMs,
    },
  }, null, 2));
} finally {
  await browser.close();
}
