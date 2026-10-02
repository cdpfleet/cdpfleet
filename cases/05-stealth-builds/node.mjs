// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const BUILDS = ['chromium', 'chrome', 'patchright', 'cloakbrowser'];

// The checks a bot-detection script typically runs, as a page script (not page.evaluate),
// exactly as a website would run them.
const CHECKS = `<script>
window.__checks = (async () => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const perm = await navigator.permissions.query({ name: 'notifications' });
  return {
    webdriver: navigator.webdriver,
    headless_in_ua: /Headless/.test(navigator.userAgent),
    window_chrome: typeof window.chrome === 'object',
    plugins: navigator.plugins.length,
    languages: navigator.languages.join(','),
    webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    notification_permission_mismatch: Notification.permission === 'denied' && perm.state === 'prompt',
    outer_minus_inner_height: outerHeight - innerHeight,
  };
})();
</script>`;

async function check(name, headless) {
  const res = await fetch(`https://starter.cdpfleet.com/${name}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: process.env.PROXY_URL, headless }),
  });
  const mode = headless ? 'headless' : 'headful';
  if (!res.ok) return { build: name, mode, error: `${res.status} ${await res.text()}` };
  const { wsUrl } = await res.json();
  const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    // A real https origin: some APIs (permissions, WebGL info) behave differently on about:blank.
    await page.route('https://detect.example/', (route) => route.fulfill({ contentType: 'text/html', body: CHECKS }));
    await page.goto('https://detect.example/');
    return { build: name, mode, version: browser.version(), ...(await page.evaluate(() => window.__checks)) };
  } finally {
    await browser.close();
  }
}

// Every build twice: headless (1 thread) and headful on a virtual display (2 threads).
console.log(JSON.stringify(await Promise.all(BUILDS.flatMap((b) => [check(b, true), check(b, false)])), null, 2));
