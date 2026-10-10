using System.Text.Json;
using Microsoft.Playwright;

namespace Cdpfleet;

/// <summary>A launched remote browser. <c>await using</c> it to close (and, for keep_alive, end) it.</summary>
public class Session : IAsyncDisposable
{
    private readonly CdpfleetClient _fleet;
    private readonly List<IBrowser> _browsers = new();
    private bool _closed;

    /// <summary>The session id (<c>sessionId</c>).</summary>
    public string Id { get; }

    /// <summary>Playwright-protocol WebSocket URL.</summary>
    public string WsUrl { get; }

    /// <summary>CDP WebSocket URL; null unless launched with <c>cdp: true</c>.</summary>
    public string? CdpUrl { get; }

    /// <summary>The browser that was launched (<c>browser</c>).</summary>
    public string? BrowserName { get; }

    /// <summary>The resource class of the session (<c>resource_class</c>).</summary>
    public string? ResourceClass { get; }

    /// <summary>Threads this session counts as (<c>weight</c>).</summary>
    public double? Weight { get; }

    /// <summary>When the session hits its overall timeout (<c>expires_at</c>).</summary>
    public string? ExpiresAt { get; }

    /// <summary>Whether the session was launched with <c>keep_alive: true</c>.</summary>
    public bool KeepAlive { get; }

    /// <summary>The full launch response.</summary>
    public JsonElement Raw { get; }

    internal Session(CdpfleetClient fleet, JsonElement raw, JsonElement options)
    {
        _fleet = fleet;
        Raw = raw;
        Id = Str(raw, "sessionId") ?? throw new CdpfleetException(200, raw, "cdpfleet: launch response has no sessionId");
        WsUrl = Str(raw, "wsUrl") ?? throw new CdpfleetException(200, raw, "cdpfleet: launch response has no wsUrl");
        CdpUrl = Str(raw, "cdpUrl");
        BrowserName = Str(raw, "browser");
        ResourceClass = Str(raw, "resource_class");
        Weight = raw.TryGetProperty("weight", out var w) && w.ValueKind == JsonValueKind.Number ? w.GetDouble() : null;
        ExpiresAt = Str(raw, "expires_at");
        KeepAlive = options.ValueKind == JsonValueKind.Object
            && options.TryGetProperty("keep_alive", out var k) && k.ValueKind == JsonValueKind.True;
    }

    private static string? Str(JsonElement el, string name) =>
        el.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String ? v.GetString() : null;

    private Dictionary<string, string> Headers(IEnumerable<KeyValuePair<string, string>>? extra)
    {
        var headers = new Dictionary<string, string>();
        if (extra is not null) foreach (var (k, v) in extra) headers[k] = v;
        headers["x-api-key"] = _fleet.ApiKey;
        return headers;
    }

    // Right after a disconnect the router may still count the old connection (409
    // session_already_connected): a keep_alive re-attach retries that for up to 10 s.
    private async Task<IBrowser> AttachAsync(Func<Task<IBrowser>> open)
    {
        for (var i = 0; ; i++)
        {
            try
            {
                var browser = await open().ConfigureAwait(false);
                _browsers.Add(browser);
                return browser;
            }
            catch (Exception e) when (i < 10 && e.Message.Contains("session_already_connected"))
            {
                await Task.Delay(1000).ConfigureAwait(false);
            }
        }
    }

    /// <summary>
    /// Connects a Playwright browser type (e.g. <c>playwright.Chromium</c>) over the Playwright protocol.
    /// Call again after a disconnect to re-attach to a keep_alive session.
    /// </summary>
    public Task<IBrowser> ConnectAsync(IBrowserType browserType, BrowserTypeConnectOptions? options = null)
    {
        var opts = options is null ? new BrowserTypeConnectOptions() : new BrowserTypeConnectOptions(options);
        opts.Headers = Headers(options?.Headers);
        return AttachAsync(() => browserType.ConnectAsync(WsUrl, opts));
    }

    /// <summary>Connects over CDP (launch with <c>cdp = true</c>): Playwright's ConnectOverCDPAsync.</summary>
    public Task<IBrowser> ConnectOverCDPAsync(IBrowserType browserType, BrowserTypeConnectOverCDPOptions? options = null)
    {
        if (CdpUrl is null) throw new InvalidOperationException("cdpfleet: this session has no cdpUrl (launch it with cdp = true)");
        var opts = options is null ? new BrowserTypeConnectOverCDPOptions() : new BrowserTypeConnectOverCDPOptions(options);
        opts.Headers = Headers(options?.Headers);
        return AttachAsync(() => browserType.ConnectOverCDPAsync(CdpUrl, opts));
    }

    private Task<JsonElement> RouterAsync(string path, object body) =>
        _fleet.SendAsync(HttpMethod.Post, $"https://{new Uri(WsUrl).Authority}{path}", body);

    /// <summary>A single-use live-view link (<c>view</c> or <c>control</c>); works before your code connects.</summary>
    public Task<JsonElement> LiveLinkAsync(string mode = "view") =>
        RouterAsync("/admin/session/live", new Dictionary<string, object?> { ["session_id"] = Id, ["mode"] = mode });

    /// <summary>Swaps the proxy of a session launched with proxy_updatable; pass the rules again (a swap replaces the route).</summary>
    public Task<JsonElement> SwapProxyAsync(object proxy, object? proxyRules = null)
    {
        var body = new Dictionary<string, object?> { ["session_id"] = Id, ["proxy"] = proxy };
        if (proxyRules is not null) body["proxy_rules"] = proxyRules;
        return RouterAsync("/admin/session/proxy", body);
    }

    /// <summary>Ends the session now (keep_alive sessions otherwise live until inactivity_timeout).</summary>
    public Task EndAsync() => _fleet.EndSessionAsync(Id);

    /// <summary>Closes what this session connected; ends keep_alive sessions too. Safe to call twice.</summary>
    public async Task CloseAsync()
    {
        if (_closed) return;
        _closed = true;
        foreach (var b in _browsers)
        {
            try { await b.CloseAsync().ConfigureAwait(false); } catch { /* already gone */ }
        }
        if (KeepAlive) await EndAsync().ConfigureAwait(false);
    }

    /// <inheritdoc cref="CloseAsync"/>
    public async ValueTask DisposeAsync()
    {
        await CloseAsync().ConfigureAwait(false);
        GC.SuppressFinalize(this);
    }
}
