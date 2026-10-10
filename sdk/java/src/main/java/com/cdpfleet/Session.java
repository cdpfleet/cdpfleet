package com.cdpfleet;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.microsoft.playwright.Browser;
import com.microsoft.playwright.BrowserType;

import java.net.URI;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Supplier;

/** A launched remote browser. Connect your own Playwright to it; {@link #close()} it when done. */
public class Session implements AutoCloseable {
  private final Cdpfleet fleet;
  private final JsonObject raw;
  private final String id;
  private final String wsUrl;
  private final String cdpUrl;
  private final String browserName;
  private final String resourceClass;
  private final Integer weight;
  private final String expiresAt;
  private final boolean keepAlive;
  private final List<Browser> browsers = new ArrayList<>();
  private boolean closed;

  Session(Cdpfleet fleet, JsonObject raw, Map<String, Object> options) {
    this.fleet = fleet;
    this.raw = raw == null ? new JsonObject() : raw;
    this.id = str("sessionId");
    this.wsUrl = str("wsUrl");
    this.cdpUrl = str("cdpUrl");
    this.browserName = str("browser");
    this.resourceClass = str("resource_class");
    JsonElement w = this.raw.get("weight");
    this.weight = w == null || w.isJsonNull() ? null : w.getAsInt();
    this.expiresAt = str("expires_at");
    this.keepAlive = Boolean.TRUE.equals(options.get("keep_alive"));
  }

  private String str(String key) {
    JsonElement e = raw.get(key);
    return e == null || e.isJsonNull() ? null : e.getAsString();
  }

  /** The session id ({@code sessionId}). */
  public String id() { return id; }
  /** The Playwright-protocol endpoint. */
  public String wsUrl() { return wsUrl; }
  /** The CDP endpoint; null unless launched with {@code cdp: true}. */
  public String cdpUrl() { return cdpUrl; }
  /** The browser that was launched ({@code browser}). */
  public String browserName() { return browserName; }
  /** The session's resource class, e.g. {@code linux_headless}. */
  public String resourceClass() { return resourceClass; }
  /** How many of the plan's threads this session uses. */
  public Integer weight() { return weight; }
  /** When the session is ended at the latest (ISO 8601). */
  public String expiresAt() { return expiresAt; }
  /** Whether the session was launched with {@code keep_alive: true}. */
  public boolean keepAlive() { return keepAlive; }
  /** The full launch response. */
  public JsonObject raw() { return raw; }

  // Right after a disconnect the router may still count the old connection (409
  // session_already_connected): a keep_alive re-attach retries that for up to 10 s.
  private Browser attach(Supplier<Browser> open) {
    for (int i = 0; ; i++) {
      try {
        Browser browser = open.get();
        browsers.add(browser);
        return browser;
      } catch (RuntimeException e) {
        String msg = e.getMessage();
        if (i >= 10 || msg == null || !msg.contains("session_already_connected")) throw e;
        Cdpfleet.sleep(1);
      }
    }
  }

  private Map<String, String> headers(Map<String, String> given) {
    Map<String, String> h = new HashMap<>();
    if (given != null) h.putAll(given);
    h.put("x-api-key", fleet.apiKey);
    return h;
  }

  /** Connects a Playwright browser type (e.g. {@code playwright.chromium()}) over the Playwright protocol. */
  public Browser connect(BrowserType browserType) { return connect(browserType, null); }

  /** {@link #connect(BrowserType)} with Playwright connect options; the API key header is added to yours. */
  public Browser connect(BrowserType browserType, BrowserType.ConnectOptions options) {
    BrowserType.ConnectOptions o = options == null ? new BrowserType.ConnectOptions() : options;
    o.setHeaders(headers(o.headers));
    return attach(() -> browserType.connect(wsUrl, o));
  }

  /** Connects over CDP (launch with {@code cdp: true}): Playwright's connectOverCDP. */
  public Browser connectOverCDP(BrowserType browserType) { return connectOverCDP(browserType, null); }

  /** {@link #connectOverCDP(BrowserType)} with Playwright options; the API key header is added to yours. */
  public Browser connectOverCDP(BrowserType browserType, BrowserType.ConnectOverCDPOptions options) {
    if (cdpUrl == null) throw new IllegalStateException("cdpfleet: this session has no cdpUrl (launch it with cdp: true)");
    BrowserType.ConnectOverCDPOptions o = options == null ? new BrowserType.ConnectOverCDPOptions() : options;
    o.setHeaders(headers(o.headers));
    return attach(() -> browserType.connectOverCDP(cdpUrl, o));
  }

  private JsonObject router(String path, Object body) {
    return Cdpfleet.object(fleet.call("POST", "https://" + URI.create(wsUrl).getRawAuthority() + path, body));
  }

  /** A single-use live-view link ({@code view}); works before your code connects. */
  public JsonObject liveLink() { return liveLink("view"); }

  /** A single-use live-view link, {@code view} or {@code control}: {@code live_url}, {@code protocol}, … */
  public JsonObject liveLink(String mode) {
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("session_id", id);
    body.put("mode", mode == null ? "view" : mode);
    return router("/admin/session/live", body);
  }

  /** Swaps the proxy of a session launched with {@code proxy_updatable}. */
  public JsonObject swapProxy(Object proxy) { return swapProxy(proxy, null); }

  /** Swaps the proxy of a session launched with {@code proxy_updatable}; pass the rules again (a swap replaces the route). */
  public JsonObject swapProxy(Object proxy, Object proxyRules) {
    Map<String, Object> body = new LinkedHashMap<>();
    body.put("session_id", id);
    body.put("proxy", proxy);
    if (proxyRules != null) body.put("proxy_rules", proxyRules);
    return router("/admin/session/proxy", body);
  }

  /** Ends the session now (keep_alive sessions otherwise live until inactivity_timeout). */
  public void end() { fleet.endSession(id); }

  /** Closes what this session connected; ends keep_alive sessions too. Safe to call twice. */
  @Override
  public void close() {
    if (closed) return;
    closed = true;
    for (Browser b : browsers) {
      try {
        b.close();
      } catch (RuntimeException ignored) {
        // already disconnected
      }
    }
    if (keepAlive) end();
  }
}
