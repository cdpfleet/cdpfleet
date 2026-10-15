#!/usr/bin/env node
// cdpfleet MCP server: gives an MCP client (Claude, Cursor, Codex…) a real remote browser on
// cdpfleet — the browser and version of its choice, a residential or your own proxy, a live
// view a person can take over, and a recording of what happened. One browser at a time.
//   env: CDPFLEET_API_KEY (required), CDPFLEET_PROXY (default proxy; else cdpfleet-resi)
// No install needed at all: the same tools are hosted at https://cdpfleet.com/mcp (src/http.js).
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import { createServer } from './tools.js';

const { server, close } = createServer({ apiKey: process.env.CDPFLEET_API_KEY, defaultProxy: process.env.CDPFLEET_PROXY });
for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, () => { close().finally(() => process.exit(0)); });
await server.connect(new StdioServerTransport());
