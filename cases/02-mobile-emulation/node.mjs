// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium, devices } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

const res = await fetch('https://starter.cdpfleet.com/chrome/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// What a page can see about the device, plus what the network sees (tls.peet.ws).
async function inspect(context) {
  const page = await context.newPage();
  const fp = await (await page.goto('https://tls.peet.ws/api/all', { timeout: 60000 })).json();
  const js = await page.evaluate(() => ({
    viewport: `${innerWidth}x${innerHeight}`,
    screen: `${screen.width}x${screen.height}`,
    device_pixel_ratio: devicePixelRatio,
    max_touch_points: navigator.maxTouchPoints,
    coarse_pointer: matchMedia('(pointer: coarse)').matches,
    platform: navigator.platform,
    ua_data_mobile: navigator.userAgentData?.mobile ?? null,
    ua_data_platform: navigator.userAgentData?.platform ?? null,
  }));
  const headers = fp.http2.sent_frames.find((f) => f.frame_type === 'HEADERS').headers;
  const header = (name) => headers.find((h) => h.startsWith(`${name}: `))?.slice(name.length + 2) ?? null;
  await page.close();
  return {
    user_agent: fp.user_agent,
    ...js,
    sec_ch_ua_mobile: header('sec-ch-ua-mobile'),
    sec_ch_ua_platform: header('sec-ch-ua-platform'),
    ja4: fp.tls.ja4,
    akamai_h2_hash: fp.http2.akamai_fingerprint_hash,
  };
}

try {
  const desktop = await browser.newContext();
  const iphone = await browser.newContext({ ...devices['iPhone 15 Pro'] });
  const pixel = await browser.newContext({ ...devices['Pixel 7'] });
  console.log(JSON.stringify({
    desktop: await inspect(desktop),
    'iPhone 15 Pro': await inspect(iphone),
    'Pixel 7': await inspect(pixel),
  }, null, 2));
} finally {
  await browser.close();
}
