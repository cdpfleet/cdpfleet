// SDK smoke test on production: launch → connect → page → live link → keep_alive reconnect → close.
// env: CDPFLEET_API_KEY (a dedicated plan for keep_alive), PROXY_URL. Prints one JSON line.
import { chromium } from 'playwright-core';
import { Cdpfleet } from '../src/index.js';

const fleet = new Cdpfleet();
const out = {};
const s1 = await fleet.launch('chromium', { proxy: process.env.PROXY_URL, headless: true, live_view: true });
const b1 = await s1.connect(chromium);
const p1 = await b1.newPage();
await p1.goto('https://httpbin.org/html');
out.title = await p1.locator('h1').textContent();
out.live_protocol = (await s1.liveLink('view')).protocol;
await s1.close();

const s2 = await fleet.launch('chromium', { proxy: process.env.PROXY_URL, headless: true, keep_alive: true, inactivity_timeout: '2m' });
let b2 = await s2.connect(chromium);
const p2 = await (await b2.newContext()).newPage();
await p2.goto('https://httpbin.org/html');
await b2.close();
b2 = await s2.connect(chromium);
out.keep_alive_page_kept = b2.contexts().flatMap((c) => c.pages()).some((p) => p.url().includes('httpbin'));
await s2.close();
out.ended = !JSON.stringify(await fleet.threads()).includes(s2.id);
out.usage_ok = typeof (await fleet.usage()).used_thread_seconds === 'number';
console.log(JSON.stringify(out));
