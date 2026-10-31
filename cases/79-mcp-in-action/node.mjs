// npm install @modelcontextprotocol/sdk@1.32.2
// env: CDPFLEET_API_KEY, PROXY_URL
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const MCP = new URL('https://cdpfleet.com/mcp');
const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;
const EXTRACT = "() => [...document.querySelectorAll('article.product_pod')].map((a) => ({ title: a.querySelector('h3 a').title, price: a.querySelector('.price_color').textContent }))";

// A script authenticates with its API key as a bearer token (assistants use OAuth instead).
async function connect() {
  const transport = new StreamableHTTPClientTransport(MCP, { requestInit: { headers: { authorization: `Bearer ${KEY}` } } });
  const client = new Client({ name: 'cdpfleet-case-79', version: '1.0.0' });
  await client.connect(transport); // initialize + initialized
  return { client, transport };
}

// One tool call, timed; the text of its result.
async function call(client, log, name, args = {}) {
  const t = Date.now();
  const r = await client.callTool({ name, arguments: args });
  const text = r.content.filter((c) => c.type === 'text').map((c) => c.text).join('\n');
  log.push({ tool: name, ms: Date.now() - t, error: Boolean(r.isError) });
  return { text, error: Boolean(r.isError) };
}

// A shared proxy drops the odd connection: one retry of a failed page load.
async function navigate(client, log, url) {
  const r = await call(client, log, 'navigate', { url });
  return r.error ? call(client, log, 'navigate', { url }) : r;
}

// ---- 1. One MCP session for the whole job, as Claude or Cursor keep one.
async function oneSession() {
  const log = [];
  let t = Date.now();
  const { client, transport } = await connect();
  log.push({ tool: 'initialize', ms: Date.now() - t, error: false });
  t = Date.now();
  const { tools } = await client.listTools();
  log.push({ tool: 'tools/list', ms: Date.now() - t, error: false });
  const out = { mcp_session: Boolean(transport.sessionId), tools: tools.map((x) => x.name).sort() };

  const launched = await call(client, log, 'launch_browser', { browser: 'chromium', headless: true, proxy: PROXY, record: false });
  // "Launched chromium 148.0.7778.5 (linux_headless, 1 thread(s)), session <id>. …"
  const m = /^Launched (\S+) (\S+) \((\w+), (\d+) thread/.exec(launched.text);
  Object.assign(out, { browser: m?.[1], version: m?.[2], resource_class: m?.[3], weight: Number(m?.[4]) });
  out.session_id = /session ([0-9a-f-]{36})/.exec(launched.text)?.[1] ?? null;
  out.navigate = (await navigate(client, log, 'https://books.toscrape.com/')).text.split('\n')[0];
  out.snapshot_chars = (await call(client, log, 'snapshot')).text.length;
  out.snapshot_list_chars = (await call(client, log, 'snapshot', { selector: 'ol.row' })).text.length;
  out.html_chars = Number((await call(client, log, 'evaluate', { expression: 'document.documentElement.outerHTML.length' })).text);
  const books = JSON.parse((await call(client, log, 'evaluate', { expression: EXTRACT })).text);
  out.books = books.length;
  out.first_book = books[0];
  out.closed = (await call(client, log, 'close_browser')).text.startsWith(`Session ${out.session_id} ended`);
  const after = await call(client, log, 'navigate', { url: 'https://books.toscrape.com/' });
  out.after_close = { error: after.error, text: after.text.split('\n')[0] };
  const account = JSON.parse((await call(client, log, 'account')).text);
  out.live_sessions_after = account.live_sessions;
  await transport.terminateSession(); // DELETE: ends the MCP session
  await client.close();
  return { ...out, calls: log };
}

// ---- 2. A new MCP session for every tool call, as ChatGPT does.
async function perCallSessions() {
  const log = [];
  const sessions = [];
  async function once(name, args) {
    const t = Date.now();
    const { client, transport } = await connect();
    const r = name === 'navigate' ? await navigate(client, log, args.url) : await call(client, log, name, args);
    log[log.length - 1].with_initialize_ms = Date.now() - t;
    sessions.push(transport.sessionId);
    await client.close(); // no DELETE: the client just goes away
    return r;
  }
  const launched = await once('launch_browser', { browser: 'chromium', headless: true, proxy: PROXY, record: false });
  const sessionId = /session ([0-9a-f-]{36})/.exec(launched.text)?.[1] ?? null;
  const nav = await once('navigate', { url: 'https://books.toscrape.com/catalogue/page-2.html' });
  const where = await once('evaluate', { expression: '({ url: location.href, books: document.querySelectorAll("article.product_pod").length })' });
  const closed = await once('close_browser', {});
  return {
    mcp_sessions: sessions.length,
    distinct_mcp_sessions: new Set(sessions).size,
    browser_session_id: sessionId,
    navigate: nav.text.split('\n')[0],
    later_session_sees: JSON.parse(where.text),
    closed_same_browser: closed.text.startsWith(`Session ${sessionId} ended`),
    calls: log,
  };
}

const one = await oneSession();
const perCall = await perCallSessions();
console.log(JSON.stringify({ language: 'node', one_session: one, per_call: perCall, measured_at: new Date().toISOString().slice(0, 10) }, null, 2));
