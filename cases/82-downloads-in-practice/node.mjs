// npm install ws @modelcontextprotocol/sdk@1.32.2
// env: CDPFLEET_API_KEY (a paid plan: kept downloads aren't on Community), PROXY_URL
import { createHash } from 'node:crypto';
import WebSocket from 'ws';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

const KEY = process.env.CDPFLEET_API_KEY;
const PROXY = process.env.PROXY_URL;
const API = 'https://cdpfleet.com';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');

// Seeded test files: the same URL always returns the same bytes, so a copy can be checked
// against the original fetched directly from here.
const PERSON_FILES = ['https://httpbingo.org/bytes/204800?seed=82', 'https://httpbingo.org/bytes/65536?seed=7'];
const AGENT_FILE = 'https://httpbingo.org/bytes/102400?seed=79';

async function api(method, path) {
  const res = await fetch(`${API}${path}`, { method, headers: { 'x-api-key': KEY } });
  return { status: res.status, body: await res.json() };
}

// Poll the account API until the session's kept files are listed; ms from `since`.
async function listed(sessionId, count, since) {
  for (;;) {
    const { body } = await api('GET', `/v1/me/downloads?session_id=${sessionId}`);
    if (body.files.length >= count || Date.now() - since > 120000) return { files: body.files, ms: Date.now() - since };
    await sleep(250);
  }
}

// Fetch each kept file through its private link and compare it with what the list said and,
// for seeded files, with the original.
async function fetchKept(files, links = {}) {
  const out = [];
  for (const f of [...files].sort((a, b) => b.bytes - a.bytes)) {
    let url = links[f.name];
    let expires = null;
    if (!url) ({ url, expires_in_s: expires } = (await api('GET', `/v1/me/downloads/${f.id}/url`)).body);
    const res = await fetch(url);
    const got = Buffer.from(await res.arrayBuffer());
    const seeded = /[?&]seed=/.test(f.source_url || '');
    const original = seeded ? Buffer.from(await (await fetch(f.source_url)).arrayBuffer()) : null;
    out.push({
      name: f.name, bytes: f.bytes, sha256: f.sha256, source_url: f.source_url,
      link: links[f.name] ? 'from the downloads tool' : `GET /v1/me/downloads/{id}/url (${expires} s)`,
      fetched_status: res.status, fetched_type: res.headers.get('content-type'), fetched_bytes: got.length, fetched_sha256: sha256(got),
      original_bytes: original?.length ?? null, original_sha256: original ? sha256(original) : null,
    });
  }
  return out;
}

// ---- 1. A person in Launch interactive: no client, the server opens the start page and
//         a person drives it through a control live link (a scripted stand-in here).
async function person() {
  const res = await fetch('https://starter.cdpfleet.com/chromium/session', {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ proxy: PROXY, headless: true, live_view: true, record: false, downloads: true, start_url: 'https://httpbingo.org/', inactivity_timeout: 60 }),
  });
  if (!res.ok) throw new Error(`launch ${res.status} ${await res.text()}`);
  const session = await res.json();
  const live = await (await fetch(`https://${new URL(session.wsUrl).host}/admin/session/live`, {
    method: 'POST',
    headers: { 'x-api-key': KEY, 'content-type': 'application/json' },
    body: JSON.stringify({ session_id: session.sessionId, mode: 'control' }),
  })).json();

  // The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
  const viewer = new WebSocket(live.live_url);
  const seen = [];
  let frames = 0;
  viewer.on('message', (data, binary) => {
    if (binary) { frames++; return; }
    const m = JSON.parse(data);
    if (m.type !== 'pages') return;
    const url = m.pages.find((p) => p.targetId === m.current)?.url;
    if (url && seen.at(-1) !== url) seen.push(url);
  });
  await new Promise((ok, fail) => { viewer.once('open', ok); viewer.once('error', fail); });
  for (let i = 0; i < 60 && !seen.includes('https://httpbingo.org/'); i++) await sleep(500);
  const startShown = seen.includes('https://httpbingo.org/');

  // The person pastes two file links into the address bar, a few seconds apart.
  for (const url of PERSON_FILES) {
    viewer.send(JSON.stringify({ type: 'navigate', url }));
    await sleep(3000);
  }
  const pageAfter = seen.at(-1);
  viewer.close();

  // Closing the dashboard's viewer ends a Launch interactive session: DELETE /v1/me/threads/{id}.
  const t = Date.now();
  const end = await api('DELETE', `/v1/me/threads/${session.sessionId}`);
  const ended = Date.now();
  const { files, ms } = await listed(session.sessionId, PERSON_FILES.length, ended);
  return {
    launch_downloads: session.downloads, resource_class: session.resource_class, live_protocol: live.protocol,
    start_page_shown: startShown, frames, pasted: PERSON_FILES, page_after: pageAfter,
    end_status: end.status, end_ms: ended - t, listed_after_ms: ms, files: await fetchKept(files),
  };
}

// ---- 2. An AI agent through the hosted MCP server, authenticated with the API key.
async function agent() {
  const transport = new StreamableHTTPClientTransport(new URL(`${API}/mcp`), { requestInit: { headers: { authorization: `Bearer ${KEY}` } } });
  const client = new Client({ name: 'cdpfleet-case-82', version: '1.0.0' });
  await client.connect(transport);
  const calls = [];
  const call = async (name, args = {}) => {
    const t = Date.now();
    const r = await client.callTool({ name, arguments: args });
    const text = r.content.filter((c) => c.type === 'text').map((c) => c.text).join('\n');
    calls.push({ tool: name, ms: Date.now() - t, error: Boolean(r.isError), text: text.split('\n')[0].slice(0, 160) });
    return text;
  };

  const launched = await call('launch_browser', { browser: 'chromium', headless: true, proxy: PROXY, record: false, downloads: true });
  const sessionId = /session ([0-9a-f-]{36})/.exec(launched)?.[1] ?? null;
  await call('navigate', { url: 'https://httpbingo.org/' });
  await call('click', { role: 'link', name: '/bytes/:n' }); // the index page's example link: 1,024 random bytes
  await call('navigate', { url: AGENT_FILE }); // a file link opened directly
  await call('evaluate', { expression: 'location.href' });
  await sleep(2000); // an assistant's next call comes seconds later; let the downloads finish
  await call('close_browser');

  // Ask the downloads tool until both files are there.
  const closed = Date.now();
  let text = '';
  let polls = 0;
  for (; Date.now() - closed < 120000; polls++) {
    text = await call('downloads');
    if ((text.match(/ bytes, sha256 /g) || []).length >= 2) break;
    await sleep(500);
  }
  const toolMs = Date.now() - closed;
  // "<name> — <bytes> bytes, sha256 <hex>" and a private link on the next line.
  const tool = [...text.matchAll(/^(.+) — (\d+) bytes, sha256 ([0-9a-f]{64})\n {2}(https:\S+)/gm)].map((m) => ({ name: m[1], bytes: Number(m[2]), sha256: m[3], link: m[4] }));
  await transport.terminateSession();
  await client.close();

  const { files } = await listed(sessionId, 2, Date.now());
  const links = Object.fromEntries(tool.map((f) => [f.name, f.link]));
  return {
    session_id_found: Boolean(sessionId),
    calls: calls.filter((c) => c.tool !== 'downloads' || c === calls.at(-1)).map((c) => (c.tool === 'downloads' ? { ...c, polls: polls + 1 } : c)),
    tool_listed_after_ms: toolMs,
    first_downloads_call: calls.find((c) => c.tool === 'downloads').text,
    tool_files: tool.map(({ link, ...f }) => ({ ...f, link_host: new URL(link).host })),
    files: await fetchKept(files, links),
  };
}

const personOut = await person();
const agentOut = await agent();
console.log(JSON.stringify({ language: 'node', person: personOut, agent: agentOut, measured_at: new Date().toISOString().slice(0, 10) }, null, 2));
