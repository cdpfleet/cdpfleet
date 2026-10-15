#!/usr/bin/env node
// Hosted cdpfleet MCP server (MCP Streamable HTTP), served at https://cdpfleet.com/mcp: the same
// tools as `npx cdpfleet-mcp`, with nothing to install. Auth, per MCP session:
//   - `Authorization: Bearer <cdpfleet API key>` (or x-api-key) on /mcp, or
//   - a personal MCP URL /mcp/u/<key> from the dashboard, for clients that can't send headers
//     (Claude's custom connectors).
// Each MCP session gets its own tools instance bound to the key that opened it; a session idle for
// MCP_IDLE_MS is closed, which ends its browser (stops billing).
//   env: PORT (8410), HOST (127.0.0.1), CDPFLEET_API_URL, MCP_IDLE_MS, MCP_MAX_PER_KEY
import http from 'node:http';
import crypto from 'node:crypto';
import { StreamableHTTPServerTransport } from '@modelcontextprotocol/sdk/server/streamableHttp.js';
import { isInitializeRequest } from '@modelcontextprotocol/sdk/types.js';
import { createServer } from './tools.js';

const PORT = Number(process.env.PORT || 8410);
const HOST = process.env.HOST || '127.0.0.1';
const API = (process.env.CDPFLEET_API_URL || 'https://cdpfleet.com').replace(/\/$/, '');
const IDLE_MS = Number(process.env.MCP_IDLE_MS || 30 * 60000);
const MAX_PER_KEY = Number(process.env.MCP_MAX_PER_KEY || 5);
const MAX_BODY = 1 << 20;
const PATH = /^\/mcp(?:\/u\/(cdpf_[\w-]{10,200}))?\/?$/;

const sessions = new Map(); // MCP session id → { transport, close, keyHash, lastUsed }
const sha = (s) => crypto.createHash('sha256').update(s).digest('hex');
const log = (msg, extra = {}) => console.log(JSON.stringify({ time: new Date().toISOString(), msg, ...extra }));

const rpcError = (res, status, message, code = -32000) => {
  res.writeHead(status, { 'content-type': 'application/json', 'cache-control': 'no-store' });
  res.end(JSON.stringify({ jsonrpc: '2.0', error: { code, message }, id: null }));
};

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

async function end(id, why) {
  const s = sessions.get(id);
  if (!s) return;
  sessions.delete(id);
  await s.close().catch(() => {});
  await s.transport.close?.().catch(() => {});
  log('mcp session closed', { session: id, why, open: sessions.size });
}

const server = http.createServer(async (req, res) => {
  const path = (req.url || '').split('?')[0];
  try {
    if (path === '/mcp/healthz') {
      res.writeHead(200, { 'content-type': 'application/json' });
      return res.end(JSON.stringify({ ok: true, sessions: sessions.size }));
    }
    const m = PATH.exec(path);
    if (!m) return rpcError(res, 404, 'not found');
    const key = keyOf(req, m[1]);
    if (!key) return rpcError(res, 401, 'Send your cdpfleet API key as "Authorization: Bearer <key>", or use your personal MCP URL from the dashboard (cdpfleet.com/app).', -32001);
    const keyHash = sha(key);
    const body = req.method === 'POST' ? await readJson(req) : undefined;
    const sid = req.headers['mcp-session-id'];

    if (sid) {
      const s = sessions.get(sid);
      if (!s) return rpcError(res, 404, 'Unknown or expired MCP session: reconnect.');
      if (s.keyHash !== keyHash) return rpcError(res, 403, 'This MCP session belongs to another key.');
      s.lastUsed = Date.now();
      return await s.transport.handleRequest(req, res, body);
    }
    if (req.method !== 'POST' || !isInitializeRequest(body)) return rpcError(res, 400, 'Start the MCP session with an initialize request.');
    if (!(await keyIsValid(key))) return rpcError(res, 401, 'Invalid or revoked cdpfleet API key.', -32001);

    // A client that reconnects often shouldn't pile up browsers: keep the newest MAX_PER_KEY.
    const mine = [...sessions.entries()].filter(([, s]) => s.keyHash === keyHash).sort((a, b) => a[1].lastUsed - b[1].lastUsed);
    while (mine.length >= MAX_PER_KEY) await end(mine.shift()[0], 'too many sessions for this key');

    const tools = createServer({ apiKey: key });
    const transport = new StreamableHTTPServerTransport({
      sessionIdGenerator: () => crypto.randomUUID(),
      onsessioninitialized: (id) => {
        sessions.set(id, { transport, close: tools.close, keyHash, lastUsed: Date.now() });
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
}, 60000).unref();

for (const sig of ['SIGINT', 'SIGTERM']) {
  process.on(sig, async () => {
    await Promise.all([...sessions.keys()].map((id) => end(id, 'server stopping')));
    process.exit(0);
  });
}

server.listen(PORT, HOST, () => log('cdpfleet MCP (HTTP) listening', { host: HOST, port: PORT }));
