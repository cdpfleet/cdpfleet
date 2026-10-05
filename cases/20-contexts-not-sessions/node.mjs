// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;

// Three visitors who must not see each other's state — in ONE browser session (1 thread).
const PERSONAS = [
  { name: 'alice', locale: 'en-US', timezone: 'America/New_York' },
  { name: 'bruno', locale: 'pt-BR', timezone: 'America/Sao_Paulo' },
  { name: 'chie', locale: 'ja-JP', timezone: 'Asia/Tokyo' },
];

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: 'new' }),
});
if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
const { wsUrl, weight } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
try {
  const contexts = [];
  for (const p of PERSONAS) {
    // Each context is a separate profile: its own cookies, storage, locale and clock.
    const context = await browser.newContext({ locale: p.locale, timezoneId: p.timezone });
    await context.addCookies([{ name: 'session', value: `${p.name}-token`, domain: 'example.com', path: '/' }]);
    const page = await context.newPage();
    await page.goto('https://example.com/', { timeout: 60000 });
    await page.evaluate((n) => localStorage.setItem('owner', n), p.name);
    contexts.push({ p, page });
  }
  const out = [];
  for (const { p, page } of contexts) {
    // Read everything after all three exist, so any leak between them would show.
    const seen = await page.evaluate(() => ({
      cookie: document.cookie,
      storage_owner: localStorage.getItem('owner'),
      language: navigator.language,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
      clock: new Date('2026-10-05T12:00:00Z').toLocaleTimeString(),
    }));
    const ip = await (await page.goto('http://ip-api.com/json/?fields=query', { timeout: 60000 })).json();
    out.push({ persona: p.name, threads: weight, ...seen, exit_ip: ip.query });
  }
  console.log(JSON.stringify(out, null, 2));
} finally {
  await browser.close();
}
