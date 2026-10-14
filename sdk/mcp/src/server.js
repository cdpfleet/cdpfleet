#!/usr/bin/env node
// cdpfleet MCP server: gives an MCP client (Claude, Cursor, Codex…) a real remote browser on
// cdpfleet — the browser and version of its choice, a residential or your own proxy, a live
// view a person can take over, and a recording of what happened. One browser at a time.
//   env: CDPFLEET_API_KEY (required), CDPFLEET_PROXY (default proxy; else cdpfleet-resi)
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { chromium, firefox, webkit } from 'playwright-core';
import { z } from 'zod';
import { Cdpfleet } from 'cdpfleet';

const BROWSERS = ['chrome', 'edge', 'brave', 'opera', 'whale', 'yandex', 'chromium', 'patchright', 'cloakbrowser', 'camoufox', 'firefox', 'webkit'];
const TYPE = { camoufox: firefox, firefox, webkit };
const MAX_TEXT = 25000;

const fleet = new Cdpfleet();
let current = null; // { session, browser, page, name }

const text = (t) => ({ content: [{ type: 'text', text: t }] });
const fail = (t) => ({ content: [{ type: 'text', text: t }], isError: true });
const clip = (s) => (s.length > MAX_TEXT ? `${s.slice(0, MAX_TEXT)}\n… (${s.length - MAX_TEXT} more characters; snapshot a narrower selector)` : s);

function need() {
  if (!current) throw new Error('No browser is open. Call launch_browser first.');
  return current;
}

// A tool body that turns exceptions into an MCP error result.
const tool = (fn) => async (args) => {
  try { return await fn(args ?? {}); } catch (err) { return fail(String(err.message || err).split('\n')[0]); }
};

// Click/type target: a CSS selector, or an ARIA role + accessible name (as in snapshot output).
const target = { selector: z.string().optional().describe('CSS selector, or text=… / role=… Playwright selector'), role: z.string().optional().describe('ARIA role from the snapshot, e.g. button, link, textbox'), name: z.string().optional().describe('Accessible name from the snapshot') };
function locate(page, { selector, role, name }) {
  if (role) return page.getByRole(role, name ? { name, exact: false } : {}).first();
  if (selector) return page.locator(selector).first();
  throw new Error('Give a selector, or a role (and name).');
}

const server = new McpServer({ name: 'cdpfleet', version: '0.1.0' });

server.registerTool('launch_browser', {
  description: 'Start a real remote browser on cdpfleet and open a page. Pick the browser the site should see. Headful by default (2 threads; less detectable). Proxy: your own proxy URL, or cdpfleet residential (default; billed per GB from your balance), optionally by country.',
  inputSchema: {
    browser: z.enum(BROWSERS).default('chrome'),
    headless: z.boolean().default(false),
    proxy: z.string().optional().describe('Proxy URL (http/https/socks5) or a cdpfleet-resi token. Default: CDPFLEET_PROXY or cdpfleet-resi'),
    country: z.string().length(2).optional().describe('Residential exit country (ISO code) when using the cdpfleet residential proxy'),
    record: z.boolean().default(true).describe('Keep a video recording of the session'),
    options: z.record(z.any()).optional().describe('Extra launch options passed unchanged (e.g. channel, version, screen_size, Camoufox os/locale)'),
  },
}, tool(async ({ browser, headless, proxy, country, record, options }) => {
  if (current) await closeCurrent();
  let p = proxy || process.env.CDPFLEET_PROXY || 'cdpfleet-resi';
  if (country && p.startsWith('cdpfleet-resi')) p = `${p}-country-${country.toLowerCase()}`;
  const live = headless && !['camoufox', 'firefox', 'webkit'].includes(browser) ? { live_view: true } : {};
  const session = await fleet.launch(browser, { proxy: p, headless, record, ...live, ...(options || {}) });
  const b = await session.connect(TYPE[browser] || chromium);
  const context = await b.newContext({ viewport: headless ? { width: 1280, height: 800 } : null });
  const page = await context.newPage();
  current = { session, browser: b, page, name: browser };
  return text(`Launched ${browser} ${b.version()} (${session.resourceClass}, ${session.weight} thread(s)), session ${session.id}. Proxy: ${p.startsWith('cdpfleet-resi') ? p : 'your proxy'}. Use navigate next.`);
}));

server.registerTool('navigate', {
  description: 'Open a URL in the current page and wait for its HTML. Returns the title and final URL.',
  inputSchema: { url: z.string().url() },
}, tool(async ({ url }) => {
  const { page } = need();
  const res = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
  return text(`${res?.status() ?? ''} ${page.url()}\nTitle: ${await page.title()}`);
}));

server.registerTool('snapshot', {
  description: 'The page (or one element) as an accessibility snapshot: roles, names, text and links in YAML — compact and good for deciding what to click.',
  inputSchema: { selector: z.string().optional().describe('Limit to this CSS selector (default: body)') },
}, tool(async ({ selector }) => {
  const { page } = need();
  return text(clip(await page.locator(selector || 'body').first().ariaSnapshot({ timeout: 15000 })));
}));

server.registerTool('click', { description: 'Click an element, by CSS selector or by role + name from the snapshot.', inputSchema: target },
  tool(async (args) => {
    const { page } = need();
    await locate(page, args).click({ timeout: 15000 });
    await page.waitForLoadState('domcontentloaded').catch(() => {});
    return text(`Clicked. Now at ${page.url()}`);
  }));

server.registerTool('type', {
  description: 'Fill a text field, by CSS selector or role + name; optionally press Enter after.',
  inputSchema: { ...target, text: z.string(), submit: z.boolean().default(false) },
}, tool(async ({ text: value, submit, ...args }) => {
  const { page } = need();
  const el = locate(page, args);
  await el.fill(value, { timeout: 15000 });
  if (submit) await el.press('Enter');
  return text(submit ? `Typed and submitted. Now at ${page.url()}` : 'Typed.');
}));

server.registerTool('press', { description: 'Press a key on the page (e.g. Enter, Escape, PageDown, ArrowDown).', inputSchema: { key: z.string() } },
  tool(async ({ key }) => { await need().page.keyboard.press(key); return text(`Pressed ${key}.`); }));

server.registerTool('wait_for', {
  description: 'Wait until an element (CSS selector) or a text appears, up to 30 s.',
  inputSchema: { selector: z.string().optional(), text: z.string().optional(), timeout_s: z.number().max(60).default(30) },
}, tool(async ({ selector, text: t, timeout_s: s }) => {
  const { page } = need();
  const loc = selector ? page.locator(selector) : t ? page.getByText(t) : null;
  if (!loc) throw new Error('Give a selector or a text.');
  await loc.first().waitFor({ timeout: s * 1000 });
  return text('It appeared.');
}));

server.registerTool('screenshot', {
  description: 'A JPEG screenshot of the current page (viewport, or the whole page).',
  inputSchema: { full_page: z.boolean().default(false) },
}, tool(async ({ full_page: fullPage }) => {
  const img = await need().page.screenshot({ type: 'jpeg', quality: 60, fullPage });
  return { content: [{ type: 'image', data: img.toString('base64'), mimeType: 'image/jpeg' }] };
}));

server.registerTool('evaluate', {
  description: 'Run a JavaScript expression in the page and return its JSON result (e.g. extract data).',
  inputSchema: { expression: z.string().describe('An expression or an arrow function, e.g. () => [...document.links].map(a => a.href)') },
}, tool(async ({ expression }) => {
  // A function given as text must be called, or evaluate returns the function itself.
  const isFn = /^\s*(async\s+)?(\([^)]*\)|[\w$]+)\s*=>|^\s*(async\s+)?function\b/.test(expression);
  const v = await need().page.evaluate(isFn ? `(${expression})()` : expression);
  return text(clip(typeof v === 'string' ? v : JSON.stringify(v, null, 2) ?? 'undefined'));
}));

server.registerTool('live_view', {
  description: 'Hand the browser to a person: e.g. to solve a captcha or type a one-time code. Returns where to watch or take control; the agent should wait until the person says they are done.',
  inputSchema: { mode: z.enum(['view', 'control']).default('control') },
}, tool(async ({ mode }) => {
  const { session, name } = need();
  const link = await session.liveLink(mode);
  return text(`A person can ${mode === 'control' ? 'take control of' : 'watch'} this ${name} session in the dashboard: https://cdpfleet.com/app#sessions → Live sessions → ${mode === 'control' ? 'Take control' : 'Watch'} (session ${session.id}).\nFor a custom viewer: ${link.protocol} link ${link.live_url} (single use, ${link.expires_in_s} s).`);
}));

server.registerTool('close_browser', { description: 'End the browser session (stops billing). The video recording appears in the dashboard (Sessions) shortly after.', inputSchema: {} },
  tool(async () => { const id = need().session.id; await closeCurrent(); return text(`Session ${id} ended.`); }));

server.registerTool('list_browsers', { description: 'The browsers cdpfleet offers, with their versions and channels.', inputSchema: {} },
  tool(async () => {
    const cat = await (await fetch('https://cdpfleet.com/api/public/browsers')).json();
    return text(cat.engines.map((e) => `${e.key}: ${e.name} — ${e.versions.map((v) => `${v.label} ${v.version}`).join(', ')}`).join('\n'));
  }));

server.registerTool('account', { description: 'Your cdpfleet plan usage today, live sessions and residential-proxy balance.', inputSchema: {} },
  tool(async () => {
    const [usage, bal, threads] = await Promise.all([fleet.usage(), fleet.proxyBalance(), fleet.threads()]);
    // The residential balance is USD, spent per GB at each tier's price (cdpfleet-resi-tier-N).
    const usd = bal.balance_usd ?? (bal.balance_bytes / 1e9) * 5;
    const gbAt = Object.fromEntries((bal.tiers || []).map((t) => [`tier_${t.tier}`, Math.round((usd / t.price_per_gb_usd) * 100) / 100]));
    return text(JSON.stringify({ usage, live_sessions: threads.threads?.length ?? 0, residential_balance_usd: Math.round(usd * 100) / 100, residential_gb_at: gbAt }, null, 2));
  }));

async function closeCurrent() {
  const c = current;
  current = null;
  if (c) await c.session.close().catch(() => {});
}

for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, () => { closeCurrent().finally(() => process.exit(0)); });
await server.connect(new StdioServerTransport());
