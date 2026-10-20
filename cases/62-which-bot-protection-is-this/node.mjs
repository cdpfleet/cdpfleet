// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
// Public pages, each loaded once. `demo` pages are the protection vendors' own test pages and are
// reported by name; for the rest only the counts per vendor are reported. `expect` is the vendor a
// first survey (2026-10-19) found on that page.
const PAGES = [
  { url: 'https://nowsecure.nl/', expect: 'turnstile', demo: 'nowsecure.nl (Turnstile test page)' },
  { url: 'https://www.scrapingcourse.com/cloudflare-challenge', expect: 'cloudflare', demo: 'scrapingcourse.com Cloudflare challenge' },
  { url: 'https://peet.ws/turnstile-test/non-interactive.html', expect: 'turnstile', demo: 'peet.ws Turnstile test (non-interactive)' },
  { url: 'https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php', expect: 'recaptcha', demo: 'Google reCAPTCHA v3 demo' },
  { url: 'https://accounts.hcaptcha.com/demo', expect: 'hcaptcha', demo: 'hCaptcha demo' },
  { url: 'https://www.crunchbase.com/', expect: 'cloudflare' },
  { url: 'https://discord.com/', expect: 'cloudflare' },
  { url: 'https://www.glassdoor.com/', expect: 'cloudflare' },
  { url: 'https://www.tripadvisor.com/', expect: 'datadome' },
  { url: 'https://www.etsy.com/', expect: 'datadome' },
  { url: 'https://soundcloud.com/', expect: 'datadome' },
  { url: 'https://www.homedepot.com/', expect: 'akamai' },
  { url: 'https://www.usps.com/', expect: 'akamai' },
  { url: 'https://www.delta.com/', expect: 'akamai' },
  { url: 'https://www.zillow.com/', expect: 'human' },
  { url: 'https://www.walmart.com/', expect: 'human' },
  { url: 'https://www.realestate.com.au/', expect: 'kasada' },
  { url: 'https://www.ticketmaster.com/', expect: 'kasada' },
  { url: 'https://www.amazon.com/', expect: 'awswaf' },
  { url: 'https://www.booking.com/', expect: 'awswaf' },
];
const VENDORS = ['cloudflare', 'turnstile', 'datadome', 'akamai', 'human', 'kasada', 'imperva', 'awswaf', 'recaptcha', 'hcaptcha'];

// [vendor, signal label, test] — each vendor leaves response headers, cookies or requests to its own hosts.
const host = (u) => { try { return new URL(u).hostname; } catch { return ''; } };
const SIGNALS = [
  ['cloudflare', 'cf-ray header', ({ h }) => 'cf-ray' in h],
  ['cloudflare', '__cf_bm / cf_clearance cookie', ({ cookies }) => cookies.some((c) => ['__cf_bm', 'cf_clearance', '__cflb'].includes(c))],
  ['cloudflare', '/cdn-cgi/challenge-platform request', ({ urls }) => urls.some((u) => u.includes('/cdn-cgi/challenge-platform'))],
  ['turnstile', 'Turnstile widget', ({ urls }) => urls.some((u) => u.includes('challenges.cloudflare.com') && u.includes('turnstile'))],
  ['datadome', 'x-datadome header', ({ h }) => 'x-datadome' in h || 'x-dd-b' in h],
  ['datadome', 'datadome cookie', ({ cookies }) => cookies.includes('datadome')],
  ['datadome', 'DataDome host (captcha-delivery.com, datadome.co)', ({ urls }) => urls.some((u) => /captcha-delivery\.com|datadome\.co/.test(host(u)))],
  ['akamai', '_abck / bm_sz cookie', ({ cookies }) => cookies.some((c) => ['_abck', 'bm_sz', 'ak_bmsc', 'bm_sv'].includes(c))],
  ['akamai', 'akamai header', ({ h }) => Object.keys(h).some((k) => k.startsWith('akamai') || k.startsWith('x-akamai'))],
  ['human', '_px cookie', ({ cookies }) => cookies.some((c) => c.startsWith('_px'))],
  ['human', 'HUMAN host (px-cdn.net, px-cloud.net)', ({ urls }) => urls.some((u) => /px-cdn\.net|px-cloud\.net|perimeterx\.net|pxchk\.net/.test(host(u)))],
  ['kasada', 'x-kpsdk header', ({ h }) => Object.keys(h).some((k) => k.startsWith('x-kpsdk'))],
  ['kasada', 'ips.js / kpsdk request', ({ urls }) => urls.some((u) => u.includes('/ips.js') || u.includes('kpsdk'))],
  ['imperva', 'x-iinfo header', ({ h }) => 'x-iinfo' in h],
  ['imperva', 'incap_ses / visid_incap cookie', ({ cookies }) => cookies.some((c) => /^(incap_ses|visid_incap|nlbi_)/.test(c))],
  ['awswaf', 'aws-waf-token cookie', ({ cookies }) => cookies.includes('aws-waf-token')],
  ['awswaf', 'AWS WAF host (awswaf.com)', ({ urls }) => urls.some((u) => /awswaf\.com/.test(host(u)))],
  ['recaptcha', 'reCAPTCHA script', ({ urls }) => urls.some((u) => u.includes('google.com/recaptcha') || u.includes('recaptcha.net'))],
  ['hcaptcha', 'hCaptcha host', ({ urls }) => urls.some((u) => /hcaptcha\.com/.test(host(u)))],
];
const CHALLENGE = /just a moment|checking your browser|verify you are human|press & hold|press and hold|are you a robot|human touch/i;
const BLOCKED = /access denied|access to this page has been denied|attention required|you have been blocked|request blocked|error 1020|pardon our interruption/i;

const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
  method: 'POST',
  headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
  body: JSON.stringify({ proxy: process.env.PROXY_URL, headless: true }),
});
if (!res.ok) throw new Error(`launch: ${res.status} ${await res.text()}`);
const { wsUrl } = await res.json();
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });

// One load per page in a fresh context: DOM ready + 6 s for the protection scripts to run, then
// read the main response's headers, the cookies and every requested URL.
async function survey(page) {
  const context = await browser.newContext();
  const tab = await context.newPage();
  const urls = [];
  tab.on('request', (r) => urls.push(r.url()));
  let h = {}, status = 0, outcome;
  try {
    const r = await tab.goto(page.url, { waitUntil: 'domcontentloaded', timeout: 45000 });
    status = r ? r.status() : 0;
    h = r ? await r.allHeaders() : {};
    await tab.waitForTimeout(6000);
    const title = await tab.title().catch(() => '');
    const text = (await tab.locator('body').innerText().catch(() => '')).replace(/\s+/g, ' ');
    const frames = tab.frames().map((f) => f.url()).join(' ');
    if (CHALLENGE.test(`${title} ${text.slice(0, 800)}`) || frames.includes('captcha-delivery.com')) outcome = 'challenge';
    else if (BLOCKED.test(`${title} ${text.slice(0, 400)}`) || ([403, 429, 444].includes(status) && text.length < 1500)) outcome = 'blocked';
    else outcome = 'content';
  } catch {
    outcome = 'error';
  }
  const cookies = (await context.cookies()).map((c) => c.name);
  await context.close();
  const hits = SIGNALS.filter(([, , test]) => test({ h, cookies, urls }));
  return { status, outcome, vendors: [...new Set(hits.map(([v]) => v))], signals: hits.map(([v, label]) => [v, label]) };
}

try {
  const vendors = Object.fromEntries(VENDORS.map((v) => [v, { vendor: v, pages: 0, signals: {}, outcomes: { content: 0, challenge: 0, blocked: 0, error: 0 } }]));
  const demoPages = [];
  let none = 0;
  let expectedMatched = 0;
  for (const page of PAGES) {
    const r = await survey(page);
    if (!r.vendors.length) none++;
    if (r.vendors.includes(page.expect)) expectedMatched++;
    for (const v of r.vendors) {
      vendors[v].pages++;
      vendors[v].outcomes[r.outcome]++;
    }
    for (const [v, label] of r.signals) vendors[v].signals[label] = (vendors[v].signals[label] || 0) + 1;
    if (page.demo) demoPages.push({ page: page.demo, vendors: r.vendors, outcome: r.outcome });
  }
  console.log(JSON.stringify({
    measured_at: new Date().toISOString().slice(0, 10),
    pages: PAGES.length,
    no_vendor_found: none,
    expected_vendor_found: expectedMatched,
    vendors: VENDORS.map((v) => vendors[v]).filter((v) => v.pages > 0),
    demo_pages: demoPages,
  }, null, 2));
} finally {
  await browser.close();
}
