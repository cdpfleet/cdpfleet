// cdpfleet MCP tools, as a factory. The browsers live in a `state` object, so several MCP
// servers can share one: the hosted server (src/http.js) keeps one state per API key, because
// some clients (ChatGPT) open a new MCP session for every tool call. The stdio server
// (src/server.js, `npx cdpfleet-mcp`) has one state of its own. A state holds one browser per name
// (operator, 2026-11-18): "default" unless a tool is given `browser_name`, so two agents on one key can
// each drive their own browser. Functions (scrape, screenshot_url, map_site, jobs) need no browser.
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { chromium, firefox, webkit } from 'playwright-core';
import { z } from 'zod';
import { Cdpfleet } from 'cdpfleet';

const BROWSERS = ['chrome', 'edge', 'brave', 'opera', 'whale', 'yandex', 'chromium', 'patchright', 'camoufox', 'firefox', 'webkit'];
const TYPE = { camoufox: firefox, firefox, webkit };
const MAX_TEXT = 25000;
const DEFAULT = 'default';
const MAX_BROWSERS = 5; // named browsers per key; each also counts against the plan's threads

// The free Community plan: a short note at launch and after a session, and with the errors its
// limits cause, saying what a paid plan adds (same wording as main's public/shared/upgrade.js).
const UPGRADE_URL = 'https://cdpfleet.com/upgrade';
const UPGRADE_LINE = 'You\'re on the free Community plan. Ready for more? Paid plans unlock more threads, longer sessions, priority capacity, recordings and persistent profiles — from $7.50 a month';
const NUDGE_EVERY_MS = 30 * 60000;
const HINTS = {
  community_capacity_full: 'The free Community capacity is busy right now — paid plans get priority capacity and aren\'t affected.',
  daily_quota_exhausted: 'That\'s today\'s free minutes used — paid plans give you 60 to 480 minutes per thread a day, or unlimited on dedicated threads.',
  hourly_quota_exhausted: 'That\'s this hour\'s share of your free minutes — paid plans come with many more.',
  threads_exceeded: 'The free plan runs 2 threads at a time — paid plans start at 5, and scale to thousands.',
  profiles_disabled: 'Persistent profiles (stay logged in across sessions) come with every paid plan.',
  keep_alive_requires_dedicated: 'keep_alive (reconnect to a running browser) comes with dedicated threads.',
  downloads_not_available: 'Kept downloads (files saved for 2 days) come with every paid plan.',
  jobs_not_available: 'Jobs (batch, crawl and monitors over many pages) come with every paid plan.',
};

// The browsers a client works with: { fleet, browsers: Map(name → { session, browser, page, name }) }.
export function createState({ apiKey, defaultProxy } = {}) {
  const state = { fleet: new Cdpfleet(apiKey ? { apiKey } : {}), defaultProxy, browsers: new Map(), plan: undefined, nudgedAt: 0 };
  // Ends one named browser, or all of them (stops billing).
  state.close = async (name) => {
    const names = name === undefined ? [...state.browsers.keys()] : [name];
    await Promise.all(names.map(async (n) => {
      const c = state.browsers.get(n);
      state.browsers.delete(n);
      if (c) await c.session.close().catch(() => {});
    }));
  };
  return state;
}

// → { server, close, state }: close() ends the open browser session (stops billing).
export function createServer({ apiKey, defaultProxy, state = createState({ apiKey, defaultProxy }) } = {}) {
  const { fleet } = state;

  const text = (t) => ({ content: [{ type: 'text', text: t }] });
  const fail = (t) => ({ content: [{ type: 'text', text: t }], isError: true });
  const clip = (s) => (s.length > MAX_TEXT ? `${s.slice(0, MAX_TEXT)}\n… (${s.length - MAX_TEXT} more characters; snapshot a narrower selector)` : s);

  // The key's plan, asked once per browser state: Community or not, and the longest idle time it allows.
  async function plan() {
    if (state.plan === undefined) {
      state.plan = await fleet.me().then((m) => ({ community: Boolean(m.subscription?.features?.community), maxIdle: m.subscription?.max_inactivity_seconds || null }), () => null);
    }
    return state.plan;
  }
  const community = async () => Boolean((await plan())?.community);
  // The upgrade note, at most every 30 minutes per key, for Community only.
  async function nudge() {
    if (!(await community()) || Date.now() - state.nudgedAt < NUDGE_EVERY_MS) return '';
    state.nudgedAt = Date.now();
    return `\n\n${UPGRADE_LINE}: ${UPGRADE_URL}`;
  }

  function need(name = DEFAULT) {
    const c = state.browsers.get(name);
    if (c) return c;
    const open = [...state.browsers.keys()];
    if (!open.length) throw new Error('No browser is open. Call launch_browser first.');
    throw new Error(`No browser named "${name}". Open: ${open.join(', ')} (pass browser_name).`);
  }
  // Which browser a tool acts on: every browser tool takes it.
  const named = { browser_name: z.string().regex(/^[A-Za-z0-9_-]{1,32}$/).optional().describe(`Which of your browsers (the name given to launch_browser). Default: "${DEFAULT}". Use different names to run several agents in parallel on one key.`) };

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

  const server = new McpServer({ name: 'cdpfleet', version: '0.2.0' });

  server.registerTool('launch_browser', {
    description: 'Start a real remote browser on cdpfleet and open a page. Pick the browser the site should see. Headful by default (2 threads; less detectable). Proxy: your own proxy URL, or cdpfleet residential (default; billed per GB from your balance), optionally by country.',
    inputSchema: {
      browser: z.enum(BROWSERS).default('chrome'),
      headless: z.boolean().default(false),
      proxy: z.string().optional().describe('Proxy URL (http/https/socks5) or a cdpfleet-resi token. Default: CDPFLEET_PROXY or cdpfleet-resi'),
      country: z.string().length(2).optional().describe('Residential exit country (ISO code) when using the cdpfleet residential proxy'),
      record: z.boolean().default(true).describe('Keep a video recording of the session'),
      downloads: z.boolean().default(false).describe('Keep every file the browser downloads, to fetch with the downloads tool after close_browser (paid plans, kept 2 days)'),
      options: z.record(z.any()).optional().describe('Extra launch options passed unchanged (e.g. channel, version, screen_size, Camoufox os/locale)'),
      ...named,
    },
  }, tool(async (args) => {
    try { return await launch(args); } catch (err) {
      const hint = HINTS[err.code] && (await community()) ? ` ${HINTS[err.code]} See plans: ${UPGRADE_URL}` : '';
      return fail(`${String(err.message || err).split('\n')[0]}${hint}`);
    }
  }));

  async function launch({ browser, headless, proxy, country, record, downloads, options, browser_name: name = DEFAULT }) {
    if (state.browsers.has(name)) await state.close(name);
    else if (state.browsers.size >= MAX_BROWSERS) throw new Error(`At most ${MAX_BROWSERS} browsers at a time (open: ${[...state.browsers.keys()].join(', ')}). Close one with close_browser.`);
    let p = proxy || state.defaultProxy || 'cdpfleet-resi';
    if (country && p.startsWith('cdpfleet-resi')) p = `${p}-country-${country.toLowerCase()}`;
    const live = headless && !['camoufox', 'firefox', 'webkit'].includes(browser) ? { live_view: true } : {};
    // A conversation pauses between tool calls: keep the browser for the plan's longest idle time
    // (shared 10 min, Community 5 min; dedicated capped at 30 min here, like the server's own idle).
    const maxIdle = (await plan())?.maxIdle;
    const idle = maxIdle ? { inactivity_timeout: Math.min(maxIdle, 1800) } : {};
    // A key created moments ago reaches the starters with the next accounts sync (about a
    // second): retry a refused key briefly instead of failing the first launch.
    let session;
    for (let i = 0; ; i++) {
      try { session = await fleet.launch(browser, { proxy: p, headless, record, ...(downloads ? { downloads: true } : {}), ...live, ...idle, ...(options || {}) }); break; } catch (err) {
        if (i >= 15 || !/invalid_api_key/.test(String(err.message || err))) throw err;
        await new Promise((r) => setTimeout(r, 1000));
      }
    }
    const b = await session.connect(TYPE[browser] || chromium);
    const context = await b.newContext({ viewport: headless ? { width: 1280, height: 800 } : null });
    const page = await context.newPage();
    state.browsers.set(name, { session, browser: b, page, name: browser });
    const called = name === DEFAULT ? '' : ` as "${name}" (pass browser_name: "${name}" to the other tools)`;
    return text(`Launched ${browser} ${b.version()}${called} (${session.resourceClass}, ${session.weight} thread(s)), session ${session.id}. Proxy: ${p.startsWith('cdpfleet-resi') ? p : p.startsWith('cdpfleet-plan') ? 'the Community proxy' : 'your proxy'}. Use navigate next.${await nudge()}`);
  }

  server.registerTool('navigate', {
    description: 'Open a URL in the current page and wait for its HTML. Returns the title and final URL.',
    inputSchema: { url: z.string().url(), ...named },
  }, tool(async ({ url, browser_name: n }) => {
    const { page } = need(n);
    const res = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 60000 });
    return text(`${res?.status() ?? ''} ${page.url()}\nTitle: ${await page.title()}`);
  }));

  server.registerTool('snapshot', {
    description: 'The page (or one element) as an accessibility snapshot: roles, names, text and links in YAML — compact and good for deciding what to click.',
    inputSchema: { selector: z.string().optional().describe('Limit to this CSS selector (default: body)'), ...named },
  }, tool(async ({ selector, browser_name: n }) => {
    const { page } = need(n);
    return text(clip(await page.locator(selector || 'body').first().ariaSnapshot({ timeout: 15000 })));
  }));

  server.registerTool('click', { description: 'Click an element, by CSS selector or by role + name from the snapshot.', inputSchema: { ...target, ...named } },
    tool(async ({ browser_name: n, ...args }) => {
      const { page } = need(n);
      await locate(page, args).click({ timeout: 15000 });
      await page.waitForLoadState('domcontentloaded').catch(() => {});
      return text(`Clicked. Now at ${page.url()}`);
    }));

  server.registerTool('type', {
    description: 'Fill a text field, by CSS selector or role + name; optionally press Enter after.',
    inputSchema: { ...target, text: z.string(), submit: z.boolean().default(false), ...named },
  }, tool(async ({ text: value, submit, browser_name: n, ...args }) => {
    const { page } = need(n);
    const el = locate(page, args);
    await el.fill(value, { timeout: 15000 });
    if (submit) await el.press('Enter');
    return text(submit ? `Typed and submitted. Now at ${page.url()}` : 'Typed.');
  }));

  server.registerTool('press', { description: 'Press a key on the page (e.g. Enter, Escape, PageDown, ArrowDown).', inputSchema: { key: z.string(), ...named } },
    tool(async ({ key, browser_name: n }) => { await need(n).page.keyboard.press(key); return text(`Pressed ${key}.`); }));

  server.registerTool('wait_for', {
    description: 'Wait until an element (CSS selector) or a text appears, up to 30 s.',
    inputSchema: { selector: z.string().optional(), text: z.string().optional(), timeout_s: z.number().max(60).default(30), ...named },
  }, tool(async ({ selector, text: t, timeout_s: s, browser_name: n }) => {
    const { page } = need(n);
    const loc = selector ? page.locator(selector) : t ? page.getByText(t) : null;
    if (!loc) throw new Error('Give a selector or a text.');
    await loc.first().waitFor({ timeout: s * 1000 });
    return text('It appeared.');
  }));

  server.registerTool('screenshot', {
    description: 'A JPEG screenshot of the current page (viewport, or the whole page).',
    inputSchema: { full_page: z.boolean().default(false), ...named },
  }, tool(async ({ full_page: fullPage, browser_name: n }) => {
    const img = await need(n).page.screenshot({ type: 'jpeg', quality: 60, fullPage });
    return { content: [{ type: 'image', data: img.toString('base64'), mimeType: 'image/jpeg' }] };
  }));

  server.registerTool('evaluate', {
    description: 'Run a JavaScript expression in the page and return its JSON result (e.g. extract data).',
    inputSchema: { expression: z.string().describe('An expression or an arrow function, e.g. () => [...document.links].map(a => a.href)'), ...named },
  }, tool(async ({ expression, browser_name: n }) => {
    // A function given as text must be called, or evaluate returns the function itself.
    const isFn = /^\s*(async\s+)?(\([^)]*\)|[\w$]+)\s*=>|^\s*(async\s+)?function\b/.test(expression);
    const v = await need(n).page.evaluate(isFn ? `(${expression})()` : expression);
    return text(clip(typeof v === 'string' ? v : JSON.stringify(v, null, 2) ?? 'undefined'));
  }));

  server.registerTool('live_view', {
    description: 'Hand the browser to a person: e.g. to solve a captcha or type a one-time code. Returns where to watch or take control; the agent should wait until the person says they are done.',
    inputSchema: { mode: z.enum(['view', 'control']).default('control'), ...named },
  }, tool(async ({ mode, browser_name: n }) => {
    const { session, name } = need(n);
    const link = await session.liveLink(mode);
    return text(`A person can ${mode === 'control' ? 'take control of' : 'watch'} this ${name} session in the dashboard: https://cdpfleet.com/app#sessions → Live sessions → ${mode === 'control' ? 'Take control' : 'Watch'} (session ${session.id}).\nFor a custom viewer: ${link.protocol} link ${link.live_url} (single use, ${link.expires_in_s} s).`);
  }));

  server.registerTool('close_browser', { description: 'End a browser session (stops billing). The video recording appears in the dashboard (Sessions) shortly after.', inputSchema: { ...named } },
    tool(async ({ browser_name: n = DEFAULT }) => { const id = need(n).session.id; state.lastSessionId = id; state.lastClosedAt = Date.now(); await state.close(n); return text(`Session ${id} ended.${await nudge()}`); }));

  server.registerTool('downloads', {
    description: 'Files kept from a session launched with downloads: true — names, sizes and a private link per file (valid 15 minutes). Files appear about a second after close_browser (this tool waits for them); kept 2 days.',
    inputSchema: { session_id: z.string().optional().describe('Default: the session closed last') },
  }, tool(async ({ session_id: sid }) => {
    const id = sid || state.lastSessionId;
    if (!id) return fail('No session yet: launch with downloads: true, download something, close_browser, then call downloads.');
    let { files = [] } = await fleet.request('GET', `/v1/me/downloads?session_id=${encodeURIComponent(id)}`);
    // Files are listed about a second after the session ends (case downloads-in-practice): right after
    // close_browser, wait up to 5 s for them rather than send the assistant away empty-handed.
    for (let i = 0; !files.length && id === state.lastSessionId && Date.now() - (state.lastClosedAt || 0) < 5000 && i < 10; i++) {
      await new Promise((r) => setTimeout(r, 500));
      ({ files = [] } = await fleet.request('GET', `/v1/me/downloads?session_id=${encodeURIComponent(id)}`));
    }
    if (!files.length) return text(`No kept files for session ${id} yet. They appear within about a minute after the session ends, and only for sessions launched with downloads: true.`);
    const links = await Promise.all(files.map((f) => fleet.request('GET', `/v1/me/downloads/${f.id}/url`).then((r) => r.url).catch(() => null)));
    return text(files.map((f, i) => `${f.name} — ${f.bytes ?? '?'} bytes, sha256 ${f.sha256 ?? '?'}${links[i] ? `\n  ${links[i]}` : ''}`).join('\n'));
  }));

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

  // Functions (cdpfleet.com/docs/functions): a page in one HTTP call, no browser to drive; billed as a short session.
  async function fn(path, body, { binary = false } = {}) {
    const res = await fetch(`${fleet.apiUrl}${path}`, { method: 'POST', headers: fleet.headers(true), body: JSON.stringify(body), signal: AbortSignal.timeout(120000) });
    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      const hint = HINTS[j.error] && (await community()) ? ` ${HINTS[j.error]} See plans: ${UPGRADE_URL}` : '';
      throw new Error(`${res.status} ${j.error || 'error'}${j.message ? `: ${j.message}` : ''}${hint}`);
    }
    return binary ? { data: Buffer.from(await res.arrayBuffer()), type: res.headers.get('content-type') || 'image/jpeg' } : res.json();
  }
  const where = {
    country: z.string().optional().describe('Country the page is loaded from (ISO code like us or de, or random; default random)'),
    max_wait: z.number().int().min(1).max(60).optional().describe('Seconds to wait for the page (default 20)'),
  };
  const only = (o) => Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined));
  const line = (x) => (typeof x === 'string' ? x : x?.href || x?.url || JSON.stringify(x));

  server.registerTool('scrape', {
    description: 'Read a web page in one call, with no browser to drive: a real browser loads it (optionally from a chosen country) and returns its Markdown, text, links or HTML, plus values picked by CSS selector. The fastest way to read a page; use launch_browser only to click through a site, log in or hand it to a person.',
    inputSchema: {
      url: z.string().url(),
      formats: z.array(z.enum(['markdown', 'text', 'links', 'html'])).default(['markdown']),
      wait_for: z.union([z.string(), z.number()]).optional().describe('A CSS selector, or milliseconds, to wait for before reading'),
      extract: z.record(z.string()).optional().describe('Values to pick: {"name": "css selector"} → that element\'s text'),
      ...where,
    },
  }, tool(async ({ url, formats, wait_for: waitFor, extract, country, max_wait: maxWait }) => {
    const r = await fn('/v1/functions/scrape', only({ url, formats, wait_for: waitFor, extract, country, max_wait: maxWait }));
    const parts = [`${r.status ?? ''} ${r.url ?? url}${r.partial ? ' (partial: the page was still loading)' : ''}`.trim()];
    if (r.extract) parts.push(`extract: ${JSON.stringify(r.extract, null, 2)}`);
    for (const f of formats) if (r[f] != null) parts.push(`--- ${f} ---\n${Array.isArray(r[f]) ? r[f].map(line).join('\n') : r[f]}`);
    return text(clip(parts.join('\n\n')));
  }));

  server.registerTool('screenshot_url', {
    description: 'A screenshot of any URL in one call, with no browser to drive (viewport, or the whole page), optionally as seen from a chosen country.',
    inputSchema: { url: z.string().url(), full_page: z.boolean().default(false), ...where },
  }, tool(async ({ url, full_page: fullPage, country, max_wait: maxWait }) => {
    const img = await fn('/v1/functions/screenshot', only({ url, full_page: fullPage, format: 'jpeg', country, max_wait: maxWait }), { binary: true });
    return { content: [{ type: 'image', data: img.data.toString('base64'), mimeType: img.type.split(';')[0] }] };
  }));

  server.registerTool('map_site', {
    description: 'List a site\'s URLs in one call: from its sitemaps (robots.txt and nested indexes) and the start page\'s links. Good before scraping or crawling a site.',
    inputSchema: {
      url: z.string().url(),
      max_urls: z.number().int().min(1).max(5000).default(500),
      include: z.array(z.string()).optional().describe('Glob patterns a URL must match, e.g. ["/blog/*"]'),
      exclude: z.array(z.string()).optional().describe('Glob patterns to leave out'),
      same_site: z.boolean().default(true),
    },
  }, tool(async ({ url, max_urls: maxUrls, include, exclude, same_site: sameSite }) => {
    const r = await fn('/v1/functions/map', only({ url, max_urls: maxUrls, include, exclude, same_site: sameSite }));
    const urls = r.urls || [];
    return text(clip(`${urls.length} URLs (sitemaps ${r.sources?.sitemap ?? 0}, links ${r.sources?.links ?? 0})\n${urls.map(line).join('\n')}`));
  }));

  server.registerTool('start_job', {
    description: 'Start a job over many pages (paid plans): "batch" scrapes a list of URLs, "crawl" follows a site\'s links from a start URL. It runs in the background; check it with job_status. Results are kept, and can be fetched page by page.',
    inputSchema: {
      type: z.enum(['batch', 'crawl']),
      urls: z.array(z.string().url()).max(10000).optional().describe('batch: the URLs to scrape'),
      url: z.string().url().optional().describe('crawl: the start URL'),
      max_depth: z.number().int().min(1).max(10).optional().describe('crawl: link depth (default 1)'),
      max_pages: z.number().int().min(1).max(100000).optional().describe('crawl: page limit (default 100)'),
      formats: z.array(z.enum(['markdown', 'text', 'links', 'html'])).default(['markdown']),
      concurrency: z.number().int().min(1).max(64).optional().describe('Pages at a time = threads the job holds (default 2)'),
    },
  }, tool(async ({ type, urls, url, max_depth: maxDepth, max_pages: maxPages, formats, concurrency }) => {
    if (type === 'batch' && !urls?.length) throw new Error('batch needs urls.');
    if (type === 'crawl' && !url) throw new Error('crawl needs url.');
    const body = type === 'batch' ? only({ urls, formats, concurrency }) : only({ url, max_depth: maxDepth, max_pages: maxPages, formats, concurrency });
    const r = await fn(`/v1/functions/${type}`, body);
    return text(`Started ${type} job ${r.job_id}. Check it with job_status (job_id "${r.job_id}"); results stay available after it ends.`);
  }));

  server.registerTool('job_status', {
    description: 'A job\'s status and progress; with results: true, also a page of its results (pass the returned cursor for the next page).',
    inputSchema: { job_id: z.string(), results: z.boolean().default(false), cursor: z.number().int().min(0).default(0) },
  }, tool(async ({ job_id: id, results, cursor }) => {
    const job = await fleet.request('GET', `/v1/jobs/${encodeURIComponent(id)}`);
    let out = JSON.stringify(job, null, 2);
    if (results) out += `\n\n--- results from ${cursor} ---\n${JSON.stringify(await fleet.request('GET', `/v1/jobs/${encodeURIComponent(id)}/results?cursor=${cursor}`), null, 2)}`;
    return text(clip(out));
  }));

  return { server, close: () => state.close(), state };
}
