// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Url = "https://books.toscrape.com/";
// Chrome DevTools' presets (throughput in bytes/s, latency in ms).
var profiles = new (string Name, Dictionary<string, object>? Conditions)[]
{
    ("no throttling", null),
    ("Fast 4G", new() { ["offline"] = false, ["latency"] = 150, ["downloadThroughput"] = 9 * 1024 * 1024 / 8.0, ["uploadThroughput"] = 1.5 * 1024 * 1024 / 8 }),
    ("Slow 3G", new() { ["offline"] = false, ["latency"] = 2000, ["downloadThroughput"] = 400 * 1024 / 8.0, ["uploadThroughput"] = 400 * 1024 / 8.0 }),
};

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var rows = new JsonArray();
    foreach (var (name, conditions) in profiles)
    {
        var context = await browser.NewContextAsync(); // no cache between runs
        var page = await context.NewPageAsync();
        // A CDP session works over the normal Playwright connection on Chromium-based browsers.
        var cdp = await context.NewCDPSessionAsync(page);
        await cdp.SendAsync("Network.enable");
        if (conditions != null) await cdp.SendAsync("Network.emulateNetworkConditions", conditions);
        var sw = System.Diagnostics.Stopwatch.StartNew();
        await page.GotoAsync(Url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 120000 });
        var htmlMs = sw.ElapsedMilliseconds;
        await page.WaitForLoadStateAsync(LoadState.Load, new() { Timeout = 180000 });
        rows.Add(new JsonObject { ["profile"] = name, ["html_ms"] = htmlMs, ["load_ms"] = sw.ElapsedMilliseconds });
        await context.CloseAsync();
    }

    // Offline: the page fails like it would on a dead connection.
    var offContext = await browser.NewContextAsync();
    var offPage = await offContext.NewPageAsync();
    var offCdp = await offContext.NewCDPSessionAsync(offPage);
    await offCdp.SendAsync("Network.enable");
    await offCdp.SendAsync("Network.emulateNetworkConditions", new() { ["offline"] = true, ["latency"] = 0, ["downloadThroughput"] = -1, ["uploadThroughput"] = -1 });
    string offline;
    try
    {
        await offPage.GotoAsync(Url, new() { Timeout = 30000 });
        offline = "loaded";
    }
    catch (PlaywrightException e)
    {
        var m = Regex.Match(e.Message, "net::[A-Z_]+");
        offline = m.Success ? m.Value : e.Message.Split('\n')[0];
    }
    await offContext.CloseAsync();

    var result = new JsonObject { ["rows"] = rows, ["offline"] = offline };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
