#!/usr/bin/env node
// Hosted cdpfleet MCP server (MCP Streamable HTTP), served at https://cdpfleet.com/mcp: the same
// tools as `npx cdpfleet-mcp`, with nothing to install. Auth, per MCP session:
//   - OAuth 2.1 (Claude, ChatGPT…): no credentials → 401 with WWW-Authenticate pointing to the
//     protected-resource metadata; the client runs cdpfleet.com's OAuth flow and sends
//     `Authorization: Bearer cdpfo_…`, checked with main (POST /internal/v1/mcp/token-info,
//     MCP_SERVICE_TOKEN), which also returns the user's hidden MCP key to launch with;
//   - `Authorization: Bearer <cdpfleet API key>` (or x-api-key) on /mcp, or
//   - a personal MCP URL /mcp/u/<key> from the dashboard, for clients that can't send headers
//     (Claude's custom connectors).
// The browser belongs to the key, not to the MCP session: some clients (ChatGPT) open a new MCP
// session for every tool call, or send calls with no session at all, and must still find the
// browser they launched. Every session of a key shares that key's browser; a key unused for
// MCP_IDLE_MS has its browser closed (stops billing). Sessions stay bound to the key that opened them.
//   env: PORT (8410), HOST (127.0.0.1), CDPFLEET_API_URL, MCP_SERVICE_TOKEN, MCP_IDLE_MS, MCP_MAX_PER_KEY
import http from 'node:http';
import crypto from 'node:crypto';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { isInitializeRequest } from '@modelcontextprotocol/sdk/types.js';
import { createServer, createState } from './tools.js';

const PORT = Number(process.env.PORT || 8410);
const HOST = process.env.HOST || '127.0.0.1';
const API = (process.env.CDPFLEET_API_URL || 'https://cdpfleet.com').replace(/\/$/, '');
const IDLE_MS = Number(process.env.MCP_IDLE_MS || 30 * 60000);
const MAX_PER_KEY = Number(process.env.MCP_MAX_PER_KEY || 5);
const MAX_BODY = 1 << 20;
const PATH = /^\/mcp(?:\/u\/(cdpf_[\w-]{10,200}))?\/?$/;

const SERVICE_TOKEN = process.env.MCP_SERVICE_TOKEN || '';
const sessions = new Map(); // MCP session id → { transport, keyHash (owner), lastUsed }
// Owner (API key hash, or "grant:<id>" for an OAuth-connected app) → its browser, shared by its sessions.
const browsers = new Map();
const sha = (s) => crypto.createHash('sha256').update(s).digest('hex');
const log = (msg, extra = {}) => console.log(JSON.stringify({ time: new Date().toISOString(), msg, ...extra }));

const rpcError = (res, status, message, code = -32000) => {
  res.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' });
  res.end(JSON.stringify({ jsonrpc: '2.0', error: { code, message }, id: null }));
};

// OAuth access token → { active, grant_id, key } from main, cached a minute.
const tokenCache = new Map();
async function tokenInfo(token) {
  const h = sha(token);
  const c = tokenCache.get(h);
  if (c && Date.now() - c.at < 60000) return c.info;
  const r = await fetch(`${API}/internal/v1/mcp/token-info`, {
    method: 'POST', headers: { authorization: `Bearer ${SERVICE_TOKEN}`, 'content-type': 'application/json' },
    body: JSON.stringify({ token }), signal: AbortSignal.timeout(10000),
  }).catch(() => null);
  const info = r?.ok ? await r.json() : { active: false };
  tokenCache.set(h, { info, at: Date.now() });
  if (tokenCache.size > 10000) tokenCache.clear();
  return info;
}

function keyOf(req, urlKey) {
  if (urlKey) return urlKey;
  const bearer = /^Bearer\s+(\S+)$/i.exec(req.headers.authorization || '')?.[1];
  return bearer || req.headers['x-api-key'] || null;
}

// Is this a live cdpfleet key? Asked of main once a minute per key at most.
const checked = new Map();
async function keyIsValid(key) {
  const h = sha(key);
  const c = checked.get(h);
  if (c && Date.now() - c.at < 60000) return c.ok;
  const r = await fetch(`${API}/v1/me`, { headers: { 'x-api-key': key }, signal: AbortSignal.timeout(10000) }).catch(() => null);
  const ok = r?.status === 200;
  checked.set(h, { ok, at: Date.now() });
  return ok;
}

function readJson(req) {
  return new Promise((resolve, reject) => {
    let size = 0;
    const chunks = [];
    req.on('data', (c) => { size += c.length; if (size > MAX_BODY) { reject(new Error('body too large')); req.destroy(); } else chunks.push(c); });
    req.on('end', () => { try { resolve(chunks.length ? JSON.parse(Buffer.concat(chunks).toString('utf8')) : undefined); } catch { reject(new Error('invalid JSON')); } });
    req.on('error', reject);
  });
}

function browserOf(key, keyHash) {
  let b = browsers.get(keyHash);
  if (!b) browsers.set(keyHash, b = { state: createState({ apiKey: key }), lastUsed: Date.now() });
  b.lastUsed = Date.now();
  return b.state;
}

async function end(id, why) {
  const s = sessions.get(id);
  if (!s) return;
  sessions.delete(id);
  await s.transport.close?.().catch(() => {});
  log('mcp session closed', { session: id, why, open: sessions.size });
}

async function closeBrowser(keyHash, why) {
  const b = browsers.get(keyHash);
  if (!b) return;
  browsers.delete(keyHash);
  const had = Boolean(b.state.current);
  await b.state.close().catch(() => {});
  if (had) log('mcp browser closed', { key: keyHash.slice(0, 12), why });
}

const server = http.createServer(async (req, res) => {
  const path = (req.url || '').split('?')[0];
  try {
    if (path === '/mcp/healthz') {
      res.writeHead(200, { 'content-type': 'application/json' });
      return res.end(JSON.stringify({ ok: true, sessions: sessions.size, browsers: [...browsers.values()].filter((b) => b.state.current).length }));
    }
    const m = PATH.exec(path);
    if (!m) return rpcError(res, 404, 'not found');
    // 401s point MCP clients to the OAuth flow (MCP authorization spec / RFC 9728).
    const unauthorized = (message, invalidToken) => {
      res.setHeader('www-authenticate', `Bearer resource_metadata="${API}/.well-known/oauth-protected-resource/mcp"${invalidToken ? ', error="invalid_token"' : ''}`);
      return rpcError(res, 401, message, -32001);
    };
    const key = keyOf(req, m[1]);
    if (!key) return unauthorized('Connect with OAuth (Claude, ChatGPT: just add https://cdpfleet.com/mcp), send "Authorization: Bearer <API key>", or use your personal MCP URL from the dashboard.');
    let apiKey = key;
    let keyHash = sha(key);
    const oauth = key.startsWith('cdpfo_');
    if (oauth) {
      const info = await tokenInfo(key);
      if (!info.active) return unauthorized('The access token is invalid, expired or revoked.', true);
      apiKey = info.key;
      keyHash = `grant:${info.grant_id}`; // refreshed tokens keep the same sessions and browser
    }
    const body = req.method === 'POST' ? await readJson(req) : undefined;
    const sid = req.headers['mcp-session-id'];

    if (sid) {
      const s = sessions.get(sid);
      if (!s) return rpcError(res, 404, 'Unknown or expired MCP session: reconnect.');
      if (s.keyHash !== keyHash) return rpcError(res, 403, 'This MCP session belongs to another key.');
      s.lastUsed = Date.now();
      browserOf(apiKey, keyHash);
      return await s.transport.handleRequest(req, res, body);
    }
    if (req.method !== 'POST') return rpcError(res, 405, 'POST JSON-RPC requests to this endpoint.');
    if (!oauth && !(await keyIsValid(key))) return unauthorized('Invalid or revoked cdpfleet API key.', true);

    // No session and not an initialize: serve it statelessly, with the key's browser.
    if (!isInitializeRequest(body)) {
      const tools = createServer({ state: browserOf(apiKey, keyHash) });
      const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined });
      await tools.server.connect(transport);
      res.on('close', () => { transport.close?.().catch(() => {}); });
      return await transport.handleRequest(req, res, body);
    }

    // A client that reconnects often shouldn't pile up browsers: keep the newest MAX_PER_KEY.
    const mine = [...sessions.entries()].filter(([, s]) => s.keyHash === keyHash).sort((a, b) => a[1].lastUsed - b[1].lastUsed);
    while (mine.length >= MAX_PER_KEY) await end(mine.shift()[0], 'too many sessions for this key');

    const tools = createServer({ state: browserOf(apiKey, keyHash) });
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => crypto.randomUUID(),
      onsessioninitialized: (id) => {
        sessions.set(id, { transport, keyHash, lastUsed: Date.now() });
        log('mcp session opened', { session: id, key: keyHash.slice(0, 12), open: sessions.size });
      },
    });
    transport.onclose = () => { if (transport.sessionId) end(transport.sessionId, 'client closed'); };
    await tools.server.connect(transport);
    await transport.handleRequest(req, res, body);
  } catch (err) {
    log('mcp request failed', { path: path.replace(/\/u\/[^/]+/, '/u/…'), err: err.message });
    if (!res.headersSent) rpcError(res, err.message === 'invalid JSON' || err.message === 'body too large' ? 400 : 500, err.message === 'invalid JSON' || err.message === 'body too large' ? err.message : 'internal error');
  }
});

setInterval(() => {
  for (const [id, s] of sessions) if (Date.now() - s.lastUsed > IDLE_MS) end(id, 'idle');
  for (const [h, b] of browsers) if (Date.now() - b.lastUsed > IDLE_MS) closeBrowser(h, 'idle');
}, 60000).unref();

for (const sig of ['SIGINT', 'SIGTERM']) {
  process.on(sig, async () => {
    await Promise.all([...sessions.keys()].map((id) => end(id, 'server stopping')));
    await Promise.all([...browsers.keys()].map((h) => closeBrowser(h, 'server stopping')));
    process.exit(0);
  });
}

server.listen(PORT, HOST, () => log('cdpfleet MCP (HTTP) listening', { host: HOST, port: PORT }));
