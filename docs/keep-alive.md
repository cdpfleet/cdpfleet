# Keeping a session alive

> **Rolling out.** `keep_alive` is being enabled across the fleet after a node rollout. Until your account's nodes have it, a launch with `"keep_alive": true` returns `503 keep_alive_unavailable` — treat it as not yet available and retry once it's live.

Normally a session ends when the last client disconnects (`browser.close()`) or after its `inactivity_timeout`. With `"keep_alive": true` the browser keeps running when your code disconnects, so you can reconnect to the **same** browser — same pages, cookies and storage — instead of launching a fresh one.

```
POST https://starter.cdpfleet.com/chrome/session
{ "proxy": "http://user:pass@proxy.example.com:8080", "keep_alive": true }
→ { "sessionId": "…", "wsUrl": "wss://router01.cdpfleet.com/session/…", … }
```

- **Dedicated threads only.** On shared capacity a `keep_alive` launch returns `403 keep_alive_requires_dedicated`. See [sessions and limits](sessions-and-limits.md) for shared vs. dedicated threads.
- **First connect:** create a context and page as usual (`browser.newContext()` / `newPage()`) — there's no default context yet.
- **Reconnect to the same `wsUrl`** (or `cdpUrl`). `browser.contexts()` lists the contexts you created earlier, with their pages, cookies and page state intact — reach them there instead of calling `newContext()` again.
- **`browser.close()` only disconnects.** With `keep_alive` it no longer ends the session; the browser stays up until you end it or it goes idle.
- **End it** with `DELETE https://cdpfleet.com/v1/me/threads/{sessionId}` (`x-api-key`), or let the session's `inactivity_timeout` expire. See the [Account API](account-api.md).

```js
// First run — do some work, then disconnect without ending the session
const browser = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const page = await browser.newPage();
await page.goto('https://example.com/login');
// …sign in…
await browser.close();                 // disconnect only — the browser stays up

// Later, reconnect to the same wsUrl
const browser2 = await chromium.connect(wsUrl, { headers: { 'x-api-key': KEY } });
const page2 = browser2.contexts()[0].pages()[0];   // still signed in
```

Invalid values for the option are rejected at launch (`invalid_keep_alive`), as is a non-boolean `record`.
