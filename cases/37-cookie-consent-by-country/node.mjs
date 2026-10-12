// npm install playwright@1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy), PROXY_URL_US (a US proxy)
import { chromium } from 'playwright';

const KEY = process.env.CDPFLEET_API_KEY;
const SITES = ['https://www.theguardian.com/international', 'https://www.bbc.com/', 'https://apnews.com/', 'https://www.theverge.com/', 'https://www.huffpost.com/'];
// First-layer buttons of the two consent platforms these sites use. OneTrust renders in the
// page; Sourcepoint in an iframe. Only a free "reject" counts: "Reject all and subscribe" doesn't.
const REJECT = { OneTrust: '#onetrust-reject-all-handler', Sourcepoint: 'button[title="Reject all"], button[title="I do not agree"]' };
const ACCEPT = { OneTrust: '#onetrust-accept-btn-handler', Sourcepoint: 'button[title="Accept all"], button[title="I agree"]' };
const SP_FRAME = 'iframe[id^="sp_message_iframe"]';
const BANNER = '#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]';

// Which consent platform the page runs, whether its banner is showing, and which consent
// APIs it exposes (__tcfapi: IAB Europe's TCF; __gpp: IAB Tech Lab's GPP, used for US state
// privacy laws).
const PROBE = `() => {
  const shown = (s) => [...document.querySelectorAll(s)].some((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
  const cmp = window.OneTrust || document.querySelector('#onetrust-consent-sdk') ? 'OneTrust'
    : window._sp_ || document.querySelector('iframe[id^="sp_message_iframe"]') ? 'Sourcepoint'
    : window.Didomi ? 'Didomi' : window.Cookiebot ? 'Cookiebot' : window.UC_UI ? 'Usercentrics' : null;
  return { cmp, banner: shown('#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'),
    tcf: typeof window.__tcfapi === 'function', gpp: typeof window.__gpp === 'function' };
}`;

async function launch(proxy) {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy, headless: false }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const { wsUrl } = await res.json();
  return chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
}

const onDomain = (host, domain) => host === domain || host.endsWith(`.${domain}`);

// Clicks a consent button; true if the click went through (one retry: a consent iframe that
// re-renders can swallow a click).
async function press(button) {
  for (let attempt = 1; attempt <= 2; attempt++) {
    try { await button.click({ timeout: 10000 }); return true; } catch { /* retry once */ }
  }
  return false;
}

// Load a site in a fresh context, then optionally click a consent button. Counts cookies and
// the hosts on other domains the page contacted (ads, analytics, CDNs) up to each moment.
async function visit(browser, url, choice) {
  const domain = new URL(url).hostname.split('.').slice(-2).join('.');
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const page = await ctx.newPage();
  const hosts = new Set();
  page.on('request', (r) => { const h = new URL(r.url()).hostname; if (h && !onDomain(h, domain)) hosts.add(h); });
  const measure = async () => {
    const cookies = await ctx.cookies();
    return { cookies: cookies.length, third_party_cookies: cookies.filter((c) => !onDomain(c.domain.replace(/^\./, ''), domain)).length, other_hosts: hosts.size };
  };
  try {
    // Proxies drop a connection now and then: one retry.
    const open = () => page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
    const res = await open().catch(open);
    // Consent scripts load late on busy pages: wait up to 20 s for the banner, and give
    // every page at least 6 s before measuring.
    const t0 = Date.now();
    const seen = await page.locator(BANNER).first().waitFor({ state: 'visible', timeout: 20000 }).then(() => true, () => false);
    const bannerAfterMs = seen ? Date.now() - t0 : null;
    if (Date.now() - t0 < 6000) await page.waitForTimeout(6000 - (Date.now() - t0));
    const probe = await page.evaluate(`(${PROBE})()`);
    const before = { status: res?.status() ?? null, ...probe, banner_after_ms: bannerAfterMs, ...(await measure()) };
    if (!choice) return { before };
    const selector = (choice === 'reject' ? REJECT : ACCEPT)[probe.cmp];
    const scope = probe.cmp === 'Sourcepoint' ? page.frameLocator(SP_FRAME) : page;
    const button = selector && scope.locator(selector).first();
    if (!button || !(await button.waitFor({ state: 'visible', timeout: 10000 }).then(() => true, () => false))) {
      return { before, after: null, missing: probe.banner ? `no ${choice} button on the first layer` : 'no banner' };
    }
    const label = (await button.getAttribute('title')) || (await button.innerText()).trim();
    await page.waitForTimeout(2000); // let the banner finish animating in, or the click can be lost
    if (!(await press(button))) return { before, after: null, missing: 'click failed' };
    // If the banner is still up, the click was lost: click once more.
    let clicks = 1;
    if (!(await page.locator(BANNER).first().waitFor({ state: 'hidden', timeout: 5000 }).then(() => true, () => false))) {
      clicks = 2;
      await press(button);
    }
    await page.waitForTimeout(6000);
    return { before, after: { button: label, clicks, ...(await measure()) } };
  } finally {
    await ctx.close();
  }
}

// A site may refuse one exit IP (an error status): visit again in a fresh context, whose new
// proxy connection gets another IP from the rotating proxy.
async function visitOnce(browser, url, choice) {
  const r = await visit(browser, url, choice);
  return r.before.status >= 200 && r.before.status < 300 ? r : visit(browser, url, choice);
}

// Sessions on shared threads end after 60 s without traffic, so the countries run one after the other.
const fromUs = [];
const us = await launch(process.env.PROXY_URL_US);
try {
  for (const url of SITES) fromUs.push(await visitOnce(us, url));
} finally {
  await us.close();
}
const sites = [];
const de = await launch(process.env.PROXY_URL_DE);
try {
  for (const [i, url] of SITES.entries()) {
    const rejected = await visitOnce(de, url, 'reject');
    const accepted = await visitOnce(de, url, 'accept');
    sites.push({
      site: new URL(url).hostname.replace(/^www\./, ''), us: fromUs[i].before, de: rejected.before,
      de_reject: rejected.after, de_reject_missing: rejected.missing ?? null, de_accept: accepted.after, de_accept_missing: accepted.missing ?? null,
    });
  }
} finally {
  await de.close();
}
console.log(JSON.stringify({ sites }, null, 2));
