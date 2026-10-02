// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US, PROXY_URL_DE, PROXY_URL_JP (exits in each country)
import { firefox } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const COUNTRIES = [
  { country: 'United States', locale: 'en-US', proxy: process.env.PROXY_URL_US },
  { country: 'Germany', locale: 'de-DE', proxy: process.env.PROXY_URL_DE },
  { country: 'Japan', locale: 'ja-JP', proxy: process.env.PROXY_URL_JP },
];

// What a localizing site reads in the page. Run in the PAGE's own JavaScript world
// ("mw:" prefix, needs main_world_eval): Playwright's default isolated world isn't patched
// the same way and can report the server's UTC timezone instead of the persona's.
const LOCAL_VIEW = `(async () => {
  const position = await new Promise((ok) => navigator.geolocation.getCurrentPosition(
    (p) => ok({ lat: p.coords.latitude, lon: p.coords.longitude }), () => ok(null), { timeout: 10000 }));
  return {
    languages: navigator.languages,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    date: new Date('2026-10-01T15:30:00Z').toLocaleString(),
    number: (1234567.891).toLocaleString(),
    price: new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(49.9),
    position,
  };
})()`;

// Great-circle distance in km.
const km = (a, b) => {
  const r = (d) => (d * Math.PI) / 180;
  const h = Math.sin(r(b.lat - a.lat) / 2) ** 2 + Math.cos(r(a.lat)) * Math.cos(r(b.lat)) * Math.sin(r(b.lon - a.lon) / 2) ** 2;
  return Math.round(12742 * Math.asin(Math.sqrt(h)));
};

async function persona({ country, locale, proxy }) {
  const res = await fetch('https://starter.cdpfleet.com/camoufox/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    // geoip: Camoufox sets timezone and geolocation from the proxy's exit IP at launch.
    body: JSON.stringify({ proxy, headless: true, os: 'windows', locale, geoip: true, main_world_eval: true }),
  });
  if (!res.ok) return { country, error: `launch ${res.status} ${await res.text()}` };
  const { wsUrl } = await res.json();
  const browser = await firefox.connect(wsUrl, { headers: { 'x-api-key': KEY } });
  try {
    const context = await browser.newContext();
    await context.grantPermissions(['geolocation']); // as if the visitor clicked "Allow"
    const page = await context.newPage();
    const exit = await (await page.goto('http://ip-api.com/json/?fields=country,city,timezone,lat,lon', { timeout: 60000 })).json();
    await page.goto('https://httpbin.org/html', { timeout: 60000 });
    const seen = await page.evaluate(`mw:${LOCAL_VIEW}`);
    const isolatedTimezone = await page.evaluate(() => Intl.DateTimeFormat().resolvedOptions().timeZone);
    return {
      country, locale,
      exit: `${exit.city}, ${exit.country}`,
      exit_timezone: exit.timezone,
      ...seen,
      position_km_from_exit: seen.position ? km(seen.position, exit) : null,
      isolated_world_timezone: isolatedTimezone, // what a default page.evaluate would have reported
    };
  } finally {
    await browser.close();
  }
}

console.log(JSON.stringify(await Promise.all(COUNTRIES.map(persona)), null, 2));
