// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITE = 'https://quotes.toscrape.com';
const NEXT = 'li.next a'; // the scraper assumes every listing page has a "Next" link

async function api(method, path) {
  const res = await fetch(`https://cdpfleet.com${path}`, { method, headers: { 'x-api-key': KEY } });
  return { status: res.status, body: await res.json().catch(() => ({})) };
}

// Headful (the default) sessions are recorded from the browser's display.
async function launch(extra = {}) {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: false, ...extra }),
  });
  if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
  const { sessionId, wsUrl } = await res.json();
  const started = Date.now();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  // viewport: null — the page fills the whole display, so it fills the recording too.
  const page = await (await browser.newContext({ viewport: null })).newPage();
  return { sessionId, started, browser, page };
}

// Proxies drop a tunnel now and then: up to two retries.
async function go(page, url) {
  for (let attempt = 1; ; attempt++) {
    try {
      return await page.goto(url, { timeout: 60000 });
    } catch (err) {
      if (attempt === 3) throw err;
      await new Promise((r) => setTimeout(r, 2000));
    }
  }
}

// The recording appears once the node has uploaded it, after the session ended.
async function waitForRecording(sessionId, maxSeconds) {
  const t = Date.now();
  while ((Date.now() - t) / 1000 < maxSeconds) {
    const rec = (await api('GET', '/v1/me/recordings')).body.recordings?.find((r) => r.session_id === sessionId);
    if (rec) return { rec, waited_s: Math.round((Date.now() - t) / 1000) };
    await new Promise((r) => setTimeout(r, 5000));
  }
  return { rec: null, waited_s: Math.round((Date.now() - t) / 1000) };
}

const out = { site: SITE, scrape: {}, recording: {}, unrecorded: {} };

// A session launched with record: false, for comparison: it must never get a recording.
const quiet = await launch({ record: false });
try {
  await go(quiet.page, SITE);
} finally {
  await quiet.browser.close();
}
out.unrecorded.session_id = quiet.sessionId;

// The scrape: pages 8, 9, 10 — follow "Next" on each. Page 10 is the last one and has none.
const s = await launch();
out.session_id = s.sessionId;
const quotes = [];
try {
  await go(s.page, `${SITE}/page/8/`);
  for (let pageNo = 8; ; pageNo++) {
    quotes.push(...await s.page.locator('.quote .text').allTextContents());
    out.scrape.last_page = pageNo;
    let href;
    try {
      href = await s.page.locator(NEXT).getAttribute('href', { timeout: 5000 }); // 5 s to find the link
    } catch (err) {
      out.scrape.failed_at_s = Math.round((Date.now() - s.started) / 100) / 10;
      out.scrape.error = `${err.name === 'TimeoutError' ? 'timeout' : err.name} waiting for ${NEXT}`;
      break;
    }
    await go(s.page, new URL(href, SITE).href); // page loads keep their own timeout and retries
  }
  out.scrape.quotes = quotes.length;
  // What the video shows at that moment: the last page — a "Previous" link and no "Next".
  out.scrape.url_at_failure = s.page.url();
  out.scrape.next_links = await s.page.locator('li.next').count();
  out.scrape.previous_links = await s.page.locator('li.previous').count();
} finally {
  await s.browser.close(); // ends the session; the node uploads the video afterwards
}
out.session_seconds = Math.round((Date.now() - s.started) / 100) / 10;

// Find the recording, check it covers the failure, download it, delete it.
const { rec, waited_s: waited } = await waitForRecording(s.sessionId, 600);
if (rec) {
  const link = (await api('GET', `/v1/me/recordings/${rec.id}/url`)).body;
  const video = await fetch(link.url);
  const bytes = Buffer.from(await video.arrayBuffer());
  out.recording = {
    appeared_after_s: waited,
    duration_seconds: rec.duration_seconds,
    width: rec.width,
    height: rec.height,
    bytes: rec.bytes,
    downloaded_bytes: bytes.length,
    content_type: video.headers.get('content-type'),
    mp4: bytes.subarray(4, 8).toString() === 'ftyp',
    link_expires_in_s: link.expires_in_s,
  };
  out.failure_within_recording = out.scrape.failed_at_s <= rec.duration_seconds + 1;
  out.recording.deleted = (await api('DELETE', `/v1/me/recordings/${rec.id}`)).status === 200;
}

// By now the scrape's video has been uploaded; give the unrecorded session another minute.
await new Promise((r) => setTimeout(r, 60000));
const list = (await api('GET', '/v1/me/recordings')).body.recordings || [];
out.unrecorded.recording_found = list.some((r) => r.session_id === quiet.sessionId);
console.log(JSON.stringify(out, null, 2));
