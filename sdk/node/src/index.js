// cdpfleet SDK for Node.js — launch a remote browser and connect your own Playwright to it.
// Spec shared by every language: ../../SPEC.md
const VERSION = '0.1.0';
const UA = `cdpfleet-sdk-node/${VERSION}`;
const QUOTA = new Set(['daily_quota_exhausted', 'hourly_quota_exhausted']);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export class CdpfleetError extends Error {
  constructor(status, body, message) {
    super(message || `cdpfleet: ${status} ${body?.error ?? ''}`.trim());
    this.name = 'CdpfleetError';
    this.status = status;
    this.code = body?.error ?? null;
    this.body = body;
  }
}

async function readJson(res) {
  const text = await res.text();
  try { return text ? JSON.parse(text) : null; } catch { return { error: text.slice(0, 200) }; }
}

export class Cdpfleet {
  constructor({ apiKey = process.env.CDPFLEET_API_KEY, starterUrl = 'https://starter.cdpfleet.com', apiUrl = 'https://cdpfleet.com', maxWait = 120 } = {}) {
    if (!apiKey) throw new Error('cdpfleet: no API key (pass apiKey or set CDPFLEET_API_KEY)');
    this.apiKey = apiKey;
    this.starterUrl = starterUrl.replace(/\/$/, '');
    this.apiUrl = apiUrl.replace(/\/$/, '');
    this.maxWait = maxWait;
  }

  headers(json = false) {
    return { 'x-api-key': this.apiKey, 'user-agent': UA, ...(json ? { 'content-type': 'application/json' } : {}) };
  }

  /** Launches a browser; retries fleet backpressure (503) and rate limits (429) as the API asks. */
  async launch(browser, options = {}) {
    const deadline = Date.now() + this.maxWait * 1000;
    for (let attempt = 1; ; attempt++) {
      let res;
      let body;
      try {
        res = await fetch(`${this.starterUrl}/${browser}/session`, {
          method: 'POST', headers: this.headers(true), body: JSON.stringify(options), signal: AbortSignal.timeout(60000),
        });
        body = await readJson(res);
      } catch (err) {
        if (Date.now() > deadline) throw new CdpfleetError(0, { error: 'network' }, `cdpfleet: launch failed: ${err.message}`);
        await sleep(Math.min(2 ** attempt, 10) * 1000);
        continue;
      }
      if (res.status === 200) return new Session(this, body, options);
      const retryable = res.status === 503 || (res.status === 429 && !QUOTA.has(body?.error));
      if (!retryable || Date.now() > deadline) throw new CdpfleetError(res.status, body);
      const after = Number(res.headers.get('retry-after')) || Math.min(2 ** (attempt - 1), 10);
      await sleep(after * 1000 + Math.random() * 500);
    }
  }

  async request(method, path, body) {
    const res = await fetch(`${this.apiUrl}${path}`, {
      method, headers: this.headers(body !== undefined), body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(30000),
    });
    const json = await readJson(res);
    if (!res.ok) throw new CdpfleetError(res.status, json);
    return json;
  }

  me() { return this.request('GET', '/v1/me'); }
  usage() { return this.request('GET', '/v1/me/usage'); }
  threads() { return this.request('GET', '/v1/me/threads'); }
  sessions({ days, limit } = {}) {
    const q = new URLSearchParams(Object.entries({ days, limit }).filter(([, v]) => v != null));
    return this.request('GET', `/v1/me/sessions${q.size ? `?${q}` : ''}`);
  }
  recordings() { return this.request('GET', '/v1/me/recordings'); }
  async recordingUrl(id) { return (await this.request('GET', `/v1/me/recordings/${id}/url`)).url; }
  deleteRecording(id) { return this.request('DELETE', `/v1/me/recordings/${id}`); }
  proxyBalance() { return this.request('GET', '/v1/me/proxy-balance'); }

  /** Ends a session at once (404 = already ended, not an error). */
  async endSession(sessionId) {
    try { await this.request('DELETE', `/v1/me/threads/${sessionId}`); } catch (err) { if (err.status !== 404) throw err; }
  }
}

export class Session {
  constructor(fleet, raw, options) {
    this.fleet = fleet;
    this.raw = raw;
    this.id = raw.sessionId;
    this.wsUrl = raw.wsUrl;
    this.cdpUrl = raw.cdpUrl ?? null;
    this.browserName = raw.browser;
    this.resourceClass = raw.resource_class;
    this.weight = raw.weight;
    this.expiresAt = raw.expires_at;
    this.keepAlive = options.keep_alive === true;
    this.browsers = [];
    this.closed = false;
  }

  // Right after a disconnect the router may still count the old connection (409
  // session_already_connected): a keep_alive re-attach retries that for up to 10 s.
  async attach(open) {
    for (let i = 0; ; i++) {
      try {
        const browser = await open();
        this.browsers.push(browser);
        return browser;
      } catch (err) {
        if (i >= 10 || !/session_already_connected/.test(err.message)) throw err;
        await sleep(1000);
      }
    }
  }

  /** Connects a Playwright browser type (e.g. `chromium` from 'playwright') over the Playwright protocol. */
  connect(browserType, options = {}) {
    return this.attach(() => browserType.connect(this.wsUrl, { ...options, headers: { ...options.headers, 'x-api-key': this.fleet.apiKey } }));
  }

  /** Connects over CDP (launch with `cdp: true`): Playwright's connectOverCDP. */
  connectOverCDP(browserType, options = {}) {
    if (!this.cdpUrl) throw new Error('cdpfleet: this session has no cdpUrl (launch it with cdp: true)');
    return this.attach(() => browserType.connectOverCDP(this.cdpUrl, { ...options, headers: { ...options.headers, 'x-api-key': this.fleet.apiKey } }));
  }

  async router(path, body) {
    const res = await fetch(`https://${new URL(this.wsUrl).host}${path}`, {
      method: 'POST', headers: this.fleet.headers(true), body: JSON.stringify(body), signal: AbortSignal.timeout(30000),
    });
    const json = await readJson(res);
    if (!res.ok) throw new CdpfleetError(res.status, json);
    return json;
  }

  /** A single-use live-view link (`view` or `control`); works before your code connects. */
  liveLink(mode = 'view') { return this.router('/admin/session/live', { session_id: this.id, mode }); }

  /** Swaps the proxy of a session launched with proxy_updatable; pass the rules again (a swap replaces the route). */
  swapProxy(proxy, proxyRules) {
    return this.router('/admin/session/proxy', { session_id: this.id, proxy, ...(proxyRules ? { proxy_rules: proxyRules } : {}) });
  }

  /** Ends the session now (keep_alive sessions otherwise live until inactivity_timeout). */
  end() { return this.fleet.endSession(this.id); }

  /** Closes what this session connected; ends keep_alive sessions too. Safe to call twice. */
  async close() {
    if (this.closed) return;
    this.closed = true;
    for (const b of this.browsers) await b.close().catch(() => {});
    if (this.keepAlive) await this.end();
  }

  async [Symbol.asyncDispose]() { await this.close(); }
}

export default Cdpfleet;
