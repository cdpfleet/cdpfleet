// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string PageUrl = "https://www.bbc.com/news";
string[] firstParty = { "bbc.com", "bbc.co.uk", "bbci.co.uk" }; // the site's own domains and CDNs
var heavy = new HashSet<string> { "image", "media", "font" };
const double PricePerGb = 3; // a typical residential proxy price, USD

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// Load the page in a fresh context (empty cache) and count every byte on the wire.
async Task<JsonObject> Measure(string label, string? block)
{
    var context = await browser.NewContextAsync();
    if (block != null)
    {
        await context.RouteAsync("**/*", route =>
        {
            var r = route.Request;
            var thirdParty = !firstParty.Any(d => new Uri(r.Url).Host.EndsWith(d));
            return heavy.Contains(r.ResourceType) || (block == "first-party" && thirdParty) ? route.AbortAsync() : route.ContinueAsync();
        });
    }
    var page = await context.NewPageAsync();
    long bytes = 0;
    int requests = 0, blocked = 0;
    var pending = new List<Task>();
    async Task Count(IRequest req)
    {
        var s = await req.SizesAsync();
        Interlocked.Add(ref bytes, s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize);
        Interlocked.Increment(ref requests);
    }
    EventHandler<IRequest> onFinished = (_, req) => { lock (pending) pending.Add(Count(req)); };
    page.RequestFinished += onFinished;
    page.RequestFailed += (_, _) => Interlocked.Increment(ref blocked);
    var sw = Stopwatch.StartNew();
    // DOM ready, then a fixed 5 s for the rest to arrive: the same window for every variant
    // ('load' can wait forever on a blocked video).
    await page.GotoAsync(PageUrl, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 90000 });
    var readyMs = sw.ElapsedMilliseconds;
    await page.WaitForTimeoutAsync(5000);
    var title = await page.TitleAsync();
    page.RequestFinished -= onFinished; // stop counting before closing
    Task[] counting;
    lock (pending) counting = pending.ToArray();
    await Task.WhenAll(counting);
    await context.CloseAsync();
    return new JsonObject
    {
        ["variant"] = label, ["title"] = title, ["requests"] = requests, ["blocked"] = blocked,
        ["kilobytes"] = (long)Math.Round(bytes / 1024.0), ["dom_ready_ms"] = readyMs,
    };
}

try
{
    var rows = new[]
    {
        await Measure("everything", null),
        await Measure("no images, media or fonts", "heavy"),
        await Measure("…and first-party only", "first-party"),
    };
    var full = rows[0]["kilobytes"]!.GetValue<long>();
    foreach (var r in rows)
    {
        double kb = r["kilobytes"]!.GetValue<long>();
        r["saved"] = $"{Math.Round((1 - kb / full) * 100)}%";
        r["proxy_cost_per_100k_pages"] = $"${Math.Round(kb * 100000 / 1024 / 1024 * PricePerGb)}";
    }
    Console.WriteLine(new JsonArray(rows.ToArray<JsonNode?>()).ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
