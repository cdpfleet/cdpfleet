// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY (residential exits come from cdpfleet-resi tokens)
import { chromium, firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const RUN = Date.now().toString(36).slice(-4);
const COUNTRIES = [
  { country: 'US', locale: 'en-US' },
  { country: 'DE', locale: 'de-DE' },
  { country: 'JP', locale: 'ja-JP' },
];
// [label, engine, launch options, context options from the exit ("match" = exit's locale + timezone)]
const SETUPS = [
  ['Camoufox (geoip on, default)', 'camoufox', { geoip: true }],
  ['Camoufox, geoip: false', 'camoufox', { geoip: false }],
  ['Camoufox, locale: "pt-BR"', 'camoufox', { geoip: true, locale: 'pt-BR' }],
  ['Chrome headful, no locale', 'chrome', { headless: false }],
  ['Chrome headful, context locale + timezoneId from the exit', 'chrome', { headless: false }, 'match'],
];

// What a site can read and compare, run on the exit-lookup page itself (ip-api.com's JSON, so the
// page knows the exit): exit country and timezone, languages, Accept-Language (echoed by httpbin),
// Intl's timezone and locale, a formatted number, and the WebRTC public address (compared with the
// exit, never returned).
const CHECK = `(async () => {
  const exit = JSON.parse(document.body.innerText);
  const headers = await fetch('https://httpbin.org/headers').then((r) => r.json()).catch(() => ({ headers: {} }));
  const srflx = await new Promise((done) => {
    const ips = new Set();
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => { const c = e.candidate && e.candidate.candidate; if (c && c.includes(' srflx ')) ips.add(c.split(' ')[4]); };
      pc.createOffer().then((o) => pc.setLocalDescription(o));
      setTimeout(() => { pc.close(); done([...ips]); }, 4000);
    } catch { done([]); }
  });
  const intl = Intl.DateTimeFormat().resolvedOptions();
  const acceptLanguage = (headers.headers['Accept-Language'] || '').split(',')[0];
  const out = {
    exit_country: exit.countryCode, exit_timezone: exit.timezone,
    languages: navigator.languages.join(', '), accept_language: acceptLanguage,
    timezone: intl.timeZone, intl_locale: intl.locale, number: (1234567.891).toLocaleString(),
    webrtc: srflx.length === 0 ? 'none' : srflx.every((ip) => ip === exit.query) ? 'exit IP' : 'another IP',
  };
  out.mismatches = [
    out.timezone !== exit.timezone && 'timezone ≠ IP timezone',
    (navigator.language.split('-')[1] || '').toUpperCase() !== exit.countryCode && 'language region ≠ IP country',
    acceptLanguage.toLowerCase() !== navigator.language.toLowerCase() && 'Accept-Language ≠ navigator.language',
    !intl.locale.toLowerCase().startsWith(navigator.language.slice(0, 2).toLowerCase()) && 'Intl locale ≠ navigator.language',
    out.webrtc === 'another IP' && 'WebRTC shows another IP',
  ].filter(Boolean);
  return out;
})()`;

async function launch(engine, body) {
  const res = await fetch(`https://starter.cdpfleet.com/${engine}/session`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${(await res.text()).slice(0, 120)}`);
  return res.json();
}

async function measure({ country, locale }, [label, engine, options, contextFrom], i) {
  const proxy = `cdpfleet-resi-country-${country.toLowerCase()}-session-c74${country.toLowerCase()}${RUN}-lifetime-10`;
  const row = { setup: label, country };
  try {
    const camoufox = engine === 'camoufox';
    const { wsUrl } = await launch(engine, { proxy, headless: camoufox, record: false, ...(camoufox ? { main_world_eval: true } : {}), ...options });
    const browser = await (camoufox ? firefox : chromium).connect(wsUrl, { headers: { 'x-api-key': KEY } });
    try {
      let contextOptions = {};
      if (contextFrom === 'match') { // look the exit up first, then set the context to match it
        const probe = await browser.newContext();
        const p = await probe.newPage();
        await p.goto('http://ip-api.com/json/?fields=timezone', { timeout: 60000 });
        contextOptions = { locale, timezoneId: JSON.parse(await p.textContent('body')).timezone };
        await probe.close();
      }
      const context = await browser.newContext(contextOptions);
      const page = await context.newPage();
      await page.goto('http://ip-api.com/json/?fields=countryCode,timezone,query', { timeout: 60000 });
      Object.assign(row, await page.evaluate(camoufox ? `mw:${CHECK}` : CHECK));
    } finally {
      await browser.close();
    }
  } catch (err) {
    row.error = err.message.split('\n')[0];
  }
  return row;
}

// Countries in parallel, setups one after another within a country (one exit per country).
const results = await Promise.all(COUNTRIES.map(async (c) => {
  const rows = [];
  for (const [i, s] of SETUPS.entries()) rows.push(await measure(c, s, i));
  return rows;
}));
console.log(JSON.stringify(results.flat(), null, 2));
