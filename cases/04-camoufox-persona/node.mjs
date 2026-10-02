// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (any exit), PROXY_URL_DE (an exit in Germany)
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// A German Windows desktop: every value below is part of one consistent story.
const persona = (proxy) => ({
  proxy,
  headless: true,
  os: 'windows',
  locale: 'de-DE',
  screen: { minWidth: 1920, maxWidth: 1920, minHeight: 1080, maxHeight: 1080 },
  window: [1600, 900],
  humanize: true,
  block_webrtc: true,
  geoip: true, // timezone and geolocation follow the proxy's exit IP
});

async function run(proxy) {
  const res = await fetch('https://starter.cdpfleet.com/camoufox/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify(persona(proxy)),
  });
  if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
  const { wsUrl } = await res.json();
  const browser = await firefox.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const page = await browser.newPage();
    const fp = await (await page.goto('https://tls.peet.ws/api/all', { timeout: 60000 })).json();
    const seen = await page.evaluate(() => {
      const gl = document.createElement('canvas').getContext('webgl');
      const dbg = gl?.getExtension('WEBGL_debug_renderer_info');
      return {
        platform: navigator.platform,
        languages: navigator.languages,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        screen: `${screen.width}x${screen.height}`,
        window: `${outerWidth}x${outerHeight}`,
        hardware_concurrency: navigator.hardwareConcurrency,
        webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
        webrtc: typeof RTCPeerConnection !== 'undefined',
      };
    });
    const headers = fp.http2.sent_frames.find((f) => f.frame_type === 'HEADERS').headers;
    // Where the proxy exits, as a website would look it up.
    const geo = await (await page.goto('http://ip-api.com/json/?fields=country,timezone', { timeout: 60000 })).json();
    return {
      exit_country: geo.country,
      exit_timezone: geo.timezone,
      user_agent: fp.user_agent,
      accept_language: headers.find((h) => h.startsWith('accept-language: '))?.slice(17) ?? null,
      ...seen,
      ja4: fp.tls.ja4,
    };
  } finally {
    await browser.close();
  }
}

console.log(JSON.stringify({
  'random exit': await run(process.env.PROXY_URL),
  'German exit': await run(process.env.PROXY_URL_DE),
}, null, 2));
