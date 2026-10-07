// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, firefox } from 'playwright';
import { writeFileSync, mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';

const KEY = process.env.CDPFLEET_API_KEY;
const dir = mkdtempSync(path.join(tmpdir(), 'cdpfleet-live-'));

const TARGETS = [
  { browser: 'chromium', type: chromium, body: { headless: 'new' } },
  { browser: 'firefox', type: firefox, body: { headless: true } },
];

async function watch({ browser: engine, type, body }) {
  let res;
  for (let attempt = 1; attempt <= 5; attempt++) { // 503 = momentarily no capacity for this browser
    res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
      method: 'POST',
      headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
      body: JSON.stringify({ proxy: process.env.PROXY_URL, ...body }),
    });
    if (res.status !== 503) break;
    await new Promise((r) => setTimeout(r, 2000 * attempt));
  }
  if (!res.ok) return { browser: engine, error: `launch ${res.status} ${await res.text()}` };
  const { wsUrl } = await res.json();
  const browser = await type.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    const frames = []; // every frame, tagged with the phase it arrived in
    let phase = 'load';
    // Every frame arrives here as JPEG bytes — this is where a viewer would get it.
    await page.screencast.start({
      quality: 60,
      onFrame: ({ data, viewportWidth, viewportHeight }) => {
        frames.push({ phase, bytes: data.length, jpeg: data[0] === 0xff && data[1] === 0xd8, w: viewportWidth, h: viewportHeight, data });
      },
    });
    for (let attempt = 1; ; attempt++) { // the proxy can drop a tunnel; retry
      try { await page.goto('https://en.wikipedia.org/wiki/Web_browser', { timeout: 60000 }); break; } catch (err) { if (attempt === 3) throw err; }
    }
    phase = 'scroll';
    const t = Date.now();
    for (let i = 0; i < 10; i++) { await page.mouse.wheel(0, 500); await page.waitForTimeout(400); }
    const scrolling = (Date.now() - t) / 1000;
    phase = 'idle'; // nothing changes on the page now
    await page.waitForTimeout(3000);
    await page.screencast.stop();
    const count = (p) => frames.filter((f) => f.phase === p).length;
    const last = frames.at(-1);
    if (last) writeFileSync(path.join(dir, `${engine}.jpg`), last.data);
    return {
      browser: engine,
      frames_while_loading: count('load'),
      frames_while_scrolling: count('scroll'),
      fps_while_scrolling: Math.round((count('scroll') / scrolling) * 10) / 10,
      frames_in_3s_idle: count('idle'),
      avg_kb: Math.round(frames.reduce((n, f) => n + f.bytes, 0) / Math.max(frames.length, 1) / 1024),
      all_jpeg: frames.every((f) => f.jpeg),
      viewport: last ? `${last.w}x${last.h}` : null,
    };
  } finally {
    await browser.close();
  }
}

const out = [];
for (const t of TARGETS) out.push(await watch(t));
console.log(JSON.stringify(out, null, 2));
