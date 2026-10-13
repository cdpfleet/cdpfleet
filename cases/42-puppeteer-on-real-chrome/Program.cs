// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
// .NET has no maintained Puppeteer port on current Chrome, so this is Playwright over CDP:
// ConnectOverCDPAsync(cdpUrl) plus raw CDP sessions for the same commands Puppeteer sends.
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Url = "https://books.toscrape.com/";
// Bytes the page pulled over the network, from its own resource timing (fresh context: no cache).
const string Transfer = """
() => {
  const all = [...performance.getEntriesByType('navigation'), ...performance.getEntriesByType('resource')];
  return { resources: all.length, transfer_kb: Math.round(all.reduce((n, e) => n + (e.transferSize || 0), 0) / 1024) };
}
""";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chrome/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new", cdp = true, record = false });
if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var s = await res.Content.ReadFromJsonAsync<JsonElement>();
var sessionId = s.GetProperty("sessionId").GetString()!;
async Task<bool> IsLive() => (await http.GetStringAsync("https://cdpfleet.com/v1/me/threads")).Contains(sessionId);

var output = new JsonObject { ["client"] = "Playwright 1.60 ConnectOverCDPAsync" };
using (var playwright = await Playwright.CreateAsync())
{
    var sw = Stopwatch.StartNew();
    var browser = await playwright.Chromium.ConnectOverCDPAsync(s.GetProperty("cdpUrl").GetString()!,
        new BrowserTypeConnectOverCDPOptions { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    output["connect_ms"] = sw.ElapsedMilliseconds;
    try
    {
        // Raw CDP at the browser level: what's really on the other end.
        var cdp = await browser.NewBrowserCDPSessionAsync();
        var version = (await cdp.SendAsync("Browser.getVersion"))!.Value;
        output["product"] = version.GetProperty("product").GetString();
        output["protocol_version"] = version.GetProperty("protocolVersion").GetString();
        async Task<int> Pages() => (await cdp.SendAsync("Target.getTargets"))!.Value.GetProperty("targetInfos")
            .EnumerateArray().Count(x => x.GetProperty("type").GetString() == "page");
        var before = await Pages();
        var full = await Load(browser, false);
        var lean = await Load(browser, true);
        output["targets"] = new JsonObject { ["pages_before"] = before, ["pages_after"] = await Pages() };
        output["loads"] = new JsonArray(full.Row, lean.Row);

        // Performance.getMetrics over a page-level CDP session (Puppeteer's page.metrics()).
        var pageCdp = await full.Context.NewCDPSessionAsync(full.Page);
        await pageCdp.SendAsync("Performance.enable");
        var m = (await pageCdp.SendAsync("Performance.getMetrics"))!.Value.GetProperty("metrics").EnumerateArray()
            .ToDictionary(x => x.GetProperty("name").GetString()!, x => x.GetProperty("value").GetDouble());
        output["metrics"] = new JsonObject { ["dom_nodes"] = (int)m["Nodes"], ["js_heap_used_mb"] = Math.Round(m["JSHeapUsedSize"] / 1048576, 1) };
        await full.Context.CloseAsync();
        await lean.Context.CloseAsync();
    }
    finally
    {
        await browser.CloseAsync(); // closes the connection, which ends the cdpfleet session
    }
}
var closed = Stopwatch.StartNew();
while (await IsLive() && closed.ElapsedMilliseconds < 60000) await Task.Delay(1000);
output["session_ended_s"] = await IsLive() ? null : JsonValue.Create(Math.Round(closed.ElapsedMilliseconds / 1000.0, 1));
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));

// One page in a fresh browser context, optionally aborting image requests.
async Task<(IBrowserContext Context, IPage Page, JsonObject Row)> Load(IBrowser browser, bool blockImages)
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    var blocked = 0;
    if (blockImages)
    {
        await page.RouteAsync("**/*", async route =>
        {
            if (route.Request.ResourceType == "image") { blocked++; await route.AbortAsync(); }
            else await route.ContinueAsync();
        });
    }
    var sw = Stopwatch.StartNew();
    for (var attempt = 1; ; attempt++) // the proxy can drop a tunnel; retry
    {
        try { await page.GotoAsync(Url, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 }); break; }
        catch (Exception e) when ((e is PlaywrightException || e is TimeoutException) && attempt < 3) { }
    }
    var loadMs = sw.ElapsedMilliseconds;
    var t = await page.EvaluateAsync<JsonElement>(Transfer);
    var row = new JsonObject
    {
        ["label"] = blockImages ? "images blocked" : "everything",
        ["resources"] = t.GetProperty("resources").GetInt32(),
        ["transfer_kb"] = t.GetProperty("transfer_kb").GetInt32(),
        ["images_blocked"] = blocked,
        ["load_ms"] = loadMs,
    };
    return (context, page, row);
}
