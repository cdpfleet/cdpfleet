# cdpfleet-mcp — MCP server for real remote browsers

Let Claude, Cursor, Codex or any MCP client drive a real browser on [cdpfleet](https://cdpfleet.com):
pick Chrome, Edge, Brave, Firefox, Camoufox and more; browse through a residential IP by country
or your own proxy; read pages as accessibility snapshots; hand the browser to a person through
live view (captchas, one-time codes); every session is recorded.

**Claude Code**

```bash
claude mcp add cdpfleet -e CDPFLEET_API_KEY=your-key -- npx -y cdpfleet-mcp
```

**Claude Desktop, Cursor and others**

```json
{ "mcpServers": { "cdpfleet": { "command": "npx", "args": ["-y", "cdpfleet-mcp"], "env": { "CDPFLEET_API_KEY": "your key" } } } }
```

Optional `CDPFLEET_PROXY`: the default proxy (else the cdpfleet residential proxy, billed per GB).

Tools: `launch_browser`, `navigate`, `snapshot`, `click`, `type`, `press`, `wait_for`,
`screenshot`, `evaluate`, `live_view`, `close_browser`, `list_browsers`, `account`.

Docs: https://cdpfleet.com/docs/mcp
