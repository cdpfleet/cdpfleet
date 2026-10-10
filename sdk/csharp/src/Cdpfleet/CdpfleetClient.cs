// cdpfleet SDK for .NET — launch a remote browser and connect your own Playwright to it.
// Spec shared by every language: ../../../SPEC.md
using System.Net;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Cdpfleet;

/// <summary>Client for the cdpfleet API: launches sessions and reads account data.</summary>
public class CdpfleetClient
{
    internal const string Version = "0.1.0";
    private const string UserAgent = "cdpfleet-sdk-csharp/" + Version;
    private static readonly HashSet<string> Quota = new() { "daily_quota_exhausted", "hourly_quota_exhausted" };

    internal static readonly JsonSerializerOptions Json = new() { DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull };
    private static readonly HttpClient Http = new(new SocketsHttpHandler { PooledConnectionLifetime = TimeSpan.FromMinutes(5) })
    {
        Timeout = Timeout.InfiniteTimeSpan,
    };

    internal string ApiKey { get; }

    /// <summary>Base URL for launches.</summary>
    public string StarterUrl { get; }

    /// <summary>Base URL for the account API.</summary>
    public string ApiUrl { get; }

    /// <summary>Seconds <see cref="LaunchAsync"/> keeps retrying 503/429.</summary>
    public int MaxWaitSeconds { get; }

    /// <param name="apiKey">Defaults to the CDPFLEET_API_KEY environment variable.</param>
    /// <param name="starterUrl">Launch endpoint base.</param>
    /// <param name="apiUrl">Account API base.</param>
    /// <param name="maxWaitSeconds">How long <see cref="LaunchAsync"/> keeps retrying.</param>
    public CdpfleetClient(string? apiKey = null, string starterUrl = "https://starter.cdpfleet.com", string apiUrl = "https://cdpfleet.com", int maxWaitSeconds = 120)
    {
        apiKey ??= Environment.GetEnvironmentVariable("CDPFLEET_API_KEY");
        if (string.IsNullOrEmpty(apiKey)) throw new ArgumentException("cdpfleet: no API key (pass apiKey or set CDPFLEET_API_KEY)", nameof(apiKey));
        ApiKey = apiKey;
        StarterUrl = starterUrl.TrimEnd('/');
        ApiUrl = apiUrl.TrimEnd('/');
        MaxWaitSeconds = maxWaitSeconds;
    }

    /// <summary>
    /// Launches a browser (<c>chrome</c>, <c>chromium</c>, <c>firefox</c>, <c>camoufox</c>, <c>webkit</c>, …).
    /// <paramref name="options"/> (anonymous object or dictionary) is sent to the API unchanged.
    /// Retries fleet backpressure (503) and rate limits (429) as the API asks, for up to <see cref="MaxWaitSeconds"/>.
    /// </summary>
    public async Task<Session> LaunchAsync(string browser, object? options = null)
    {
        var body = JsonSerializer.SerializeToElement(options ?? new Dictionary<string, object?>(), Json);
        var payload = body.GetRawText();
        var deadline = DateTime.UtcNow.AddSeconds(MaxWaitSeconds);
        for (var attempt = 1; ; attempt++)
        {
            HttpResponseMessage res;
            JsonElement? json;
            try
            {
                using var req = Request(HttpMethod.Post, $"{StarterUrl}/{browser}/session", payload);
                using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(60));
                res = await Http.SendAsync(req, cts.Token).ConfigureAwait(false);
                json = await ReadJsonAsync(res, cts.Token).ConfigureAwait(false);
            }
            catch (Exception e) when (e is HttpRequestException or TaskCanceledException or IOException)
            {
                if (DateTime.UtcNow > deadline)
                    throw new CdpfleetException(0, JsonSerializer.SerializeToElement(new { error = "network" }), $"cdpfleet: launch failed: {e.Message}");
                await Task.Delay(TimeSpan.FromSeconds(Math.Min(Math.Pow(2, attempt), 10))).ConfigureAwait(false);
                continue;
            }
            using (res)
            {
                var status = (int)res.StatusCode;
                if (status == 200 && json is { } raw) return new Session(this, raw, body);
                var code = CdpfleetException.ErrorOf(json);
                var retryable = status == 503 || (status == 429 && (code is null || !Quota.Contains(code)));
                if (!retryable || DateTime.UtcNow > deadline) throw new CdpfleetException(status, json);
                var after = RetryAfterSeconds(res) ?? Math.Min(Math.Pow(2, attempt - 1), 10);
                await Task.Delay(TimeSpan.FromMilliseconds(after * 1000 + Random.Shared.NextDouble() * 500)).ConfigureAwait(false);
            }
        }
    }

    private static double? RetryAfterSeconds(HttpResponseMessage res)
    {
        if (res.Headers.RetryAfter?.Delta is { } delta && delta.TotalSeconds > 0) return delta.TotalSeconds;
        if (res.Headers.TryGetValues("retry-after", out var values)
            && double.TryParse(values.FirstOrDefault(), System.Globalization.NumberStyles.Float, System.Globalization.CultureInfo.InvariantCulture, out var s)
            && s > 0)
            return s;
        return null;
    }

    internal HttpRequestMessage Request(HttpMethod method, string url, string? json)
    {
        var req = new HttpRequestMessage(method, url);
        req.Headers.TryAddWithoutValidation("x-api-key", ApiKey);
        req.Headers.TryAddWithoutValidation("user-agent", UserAgent);
        if (json is not null) req.Content = new StringContent(json, Encoding.UTF8, "application/json");
        return req;
    }

    // Empty body → null; a body that is not JSON → { "error": "<first 200 chars>" }.
    internal static async Task<JsonElement?> ReadJsonAsync(HttpResponseMessage res, CancellationToken ct)
    {
        var text = await res.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        if (string.IsNullOrEmpty(text)) return null;
        try { return JsonDocument.Parse(text).RootElement.Clone(); }
        catch (JsonException) { return JsonSerializer.SerializeToElement(new { error = text.Length > 200 ? text[..200] : text }); }
    }

    internal async Task<JsonElement> SendAsync(HttpMethod method, string url, object? body = null)
    {
        using var req = Request(method, url, body is null ? null : JsonSerializer.Serialize(body, Json));
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(30));
        using var res = await Http.SendAsync(req, cts.Token).ConfigureAwait(false);
        var json = await ReadJsonAsync(res, cts.Token).ConfigureAwait(false);
        if (!res.IsSuccessStatusCode) throw new CdpfleetException((int)res.StatusCode, json);
        return json ?? default;
    }

    private Task<JsonElement> GetAsync(string path) => SendAsync(HttpMethod.Get, ApiUrl + path);

    /// <summary><c>GET /v1/me</c>: the account.</summary>
    public Task<JsonElement> MeAsync() => GetAsync("/v1/me");

    /// <summary><c>GET /v1/me/usage</c>: thread-seconds used and left.</summary>
    public Task<JsonElement> UsageAsync() => GetAsync("/v1/me/usage");

    /// <summary><c>GET /v1/me/threads</c>: running sessions.</summary>
    public Task<JsonElement> ThreadsAsync() => GetAsync("/v1/me/threads");

    /// <summary><c>GET /v1/me/sessions</c>: session history.</summary>
    public Task<JsonElement> SessionsAsync(int? days = null, int? limit = null)
    {
        var q = new List<string>();
        if (days is not null) q.Add($"days={days}");
        if (limit is not null) q.Add($"limit={limit}");
        return GetAsync("/v1/me/sessions" + (q.Count > 0 ? "?" + string.Join("&", q) : ""));
    }

    /// <summary><c>GET /v1/me/recordings</c>: session recordings.</summary>
    public Task<JsonElement> RecordingsAsync() => GetAsync("/v1/me/recordings");

    /// <summary>A download URL for a recording.</summary>
    public async Task<string> RecordingUrlAsync(string id)
    {
        var json = await GetAsync($"/v1/me/recordings/{Uri.EscapeDataString(id)}/url").ConfigureAwait(false);
        return json.GetProperty("url").GetString()!;
    }

    /// <summary><c>DELETE /v1/me/recordings/{id}</c>.</summary>
    public Task<JsonElement> DeleteRecordingAsync(string id) =>
        SendAsync(HttpMethod.Delete, $"{ApiUrl}/v1/me/recordings/{Uri.EscapeDataString(id)}");

    /// <summary><c>GET /v1/me/proxy-balance</c>: built-in residential proxy balance.</summary>
    public Task<JsonElement> ProxyBalanceAsync() => GetAsync("/v1/me/proxy-balance");

    /// <summary>Ends a session at once (404 = already ended, not an error).</summary>
    public async Task EndSessionAsync(string sessionId)
    {
        try { await SendAsync(HttpMethod.Delete, $"{ApiUrl}/v1/me/threads/{Uri.EscapeDataString(sessionId)}").ConfigureAwait(false); }
        catch (CdpfleetException e) when (e.Status == (int)HttpStatusCode.NotFound) { }
    }
}
