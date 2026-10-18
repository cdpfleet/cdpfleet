// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var sites = new[]
{
    ("The Guardian", "https://www.theguardian.com/international"),
    ("NPR", "https://www.npr.org/"),
    ("Wikipedia", "https://en.wikipedia.org/wiki/Web_browser"),
};
// Analytics, measurement and real-user-monitoring hosts (subdomains included) — not ads.
string[] telemetryHosts = { "google-analytics.com", "analytics.google.com", "googletagmanager.com", "ophan.theguardian.com",
    "performance-events.guardianapis.com", "feast-events.guardianapis.com", "scorecardresearch.com", "permutive.app",
    "permutive.com", "go-mpulse.net", "akstat.io", "nr-data.net", "newrelic.com", "sentry.io", "datadoghq.com",
    "hotjar.com", "clarity.ms", "segment.io", "segment.com", "chartbeat.com", "chartbeat.net", "parsely.com",
    "analytics.yahoo.com", "omtrdc.net", "demdex.net", "bat.bing.com", "cloudflareinsights.com", "brandmetrics.com" };

// Telemetry: one of those hosts, or a beacon (navigator.sendBeacon, <a ping>).
bool IsTelemetry(IRequest req)
{
    if (req.ResourceType == "ping") return true;
    var host = new Uri(req.Url).Host;
    return telemetryHosts.Any(d => host == d || host.EndsWith("." + d));
}

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL_US"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// One load in a fresh context: DOM ready + 6 s, then leave the page (about:blank) and give the
// page-hide beacons 1.5 s. Counts every request, the telemetry among them, and bytes on the wire.
async Task<Dictionary<string, object>> Load(string url, bool block)
{
    var context = await browser.NewContextAsync();
    long requests = 0, telemetry = 0, onLeave = 0, bytes = 0, telemetryBytes = 0, blocked = 0;
    var leaving = false;
    if (block)
    {
        await context.RouteAsync("**/*", route =>
        {
            if (!IsTelemetry(route.Request)) return route.ContinueAsync();
            Interlocked.Increment(ref blocked);
            return route.AbortAsync();
        });
    }
    var page = await context.NewPageAsync();
    page.Request += (_, req) =>
    {
        Interlocked.Increment(ref requests);
        if (!IsTelemetry(req)) return;
        Interlocked.Increment(ref telemetry);
        if (leaving) Interlocked.Increment(ref onLeave);
    };
    var sizing = new List<Task>(); // SizesAsync is async: wait for them all before closing the context
    async Task Size(IRequest req)
    {
        try
        {
            var s = await req.SizesAsync();
            var n = s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize;
            Interlocked.Add(ref bytes, n);
            if (IsTelemetry(req)) Interlocked.Add(ref telemetryBytes, n);
        }
        catch (PlaywrightException) { }
    }
    page.RequestFinished += (_, req) => { lock (sizing) sizing.Add(Size(req)); };
    var sw = Stopwatch.StartNew();
    await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 90000 });
    var domReady = sw.ElapsedMilliseconds;
    await page.WaitForTimeoutAsync(6000);
    var title = await page.TitleAsync();
    var headings = await page.Locator("h1, h2, h3").CountAsync(); // the content is still there
    leaving = true;
    await page.GotoAsync("about:blank");
    await page.WaitForTimeoutAsync(1500);
    Task[] pending;
    lock (sizing) pending = sizing.ToArray();
    await Task.WhenAll(pending);
    await context.CloseAsync();
    return new()
    {
        ["requests"] = requests, ["telemetry"] = telemetry, ["on_leave"] = onLeave, ["bytes"] = bytes, ["telemetry_bytes"] = telemetryBytes,
        ["blocked"] = blocked, ["dom_ready"] = domReady, ["title"] = title, ["headings"] = headings,
    };
}

try
{
    var rows = new JsonArray();
    foreach (var (name, url) in sites)
    {
        var off = await Load(url, false);
        var on = await Load(url, true);
        var title = (string)off["title"];
        rows.Add(new JsonObject
        {
            ["site"] = name,
            ["title_ok"] = title.Length > 0 && title == (string)on["title"],
            ["requests"] = (long)off["requests"],
            ["telemetry_requests"] = (long)off["telemetry"],
            ["telemetry_on_leave"] = (long)off["on_leave"],
            ["telemetry_kb"] = (long)Math.Round((long)off["telemetry_bytes"] / 1024.0),
            ["total_kb"] = (long)Math.Round((long)off["bytes"] / 1024.0),
            ["blocked"] = (long)on["blocked"],
            ["headings"] = (int)off["headings"],
            ["headings_blocked_run"] = (int)on["headings"],
            ["dom_ready_ms"] = (long)off["dom_ready"],
            ["dom_ready_ms_blocked_run"] = (long)on["dom_ready"],
        });
    }
    Console.WriteLine(rows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
