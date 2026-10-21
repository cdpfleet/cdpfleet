// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Url = "https://en.wikipedia.org/wiki/Web_browser";
int[] rates = [1, 4, 6];
// A fixed amount of JavaScript work, timed inside the page: what a heavy client-side app does.
const string Work = """
(() => {
  const t = performance.now();
  let h = 0;
  for (let i = 0; i < 3e6; i++) h = (h * 31 + i) % 1000003;
  const a = Array.from({ length: 200000 }, (_, i) => (i * 7919) % 100003).sort((x, y) => x - y);
  return { ms: Math.round(performance.now() - t), check: h + a[1000] };
})()
""";
const string Nav = """
() => {
  const n = performance.getEntriesByType('navigation')[0];
  return { response_ms: Math.round(n.responseEnd), dcl_ms: Math.round(n.domContentLoadedEventEnd), load_ms: Math.round(n.loadEventEnd) };
}
""";

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
    foreach (var rate in rates)
    {
        var context = await browser.NewContextAsync();
        var page = await context.NewPageAsync();
        // CPU throttling is a DevTools feature: reach it with a CDP session over the Playwright connection.
        var cdp = await context.NewCDPSessionAsync(page);
        await cdp.SendAsync("Performance.enable");
        await cdp.SendAsync("Emulation.setCPUThrottlingRate", new() { ["rate"] = rate });
        // A real page load: the network part isn't throttled, the main-thread work is.
        await page.GotoAsync(Url, new() { WaitUntil = WaitUntilState.Load, Timeout = 90000 });
        var nav = await page.EvaluateAsync<JsonElement>(Nav);
        var metrics = (await cdp.SendAsync("Performance.getMetrics"))!.Value.GetProperty("metrics").EnumerateArray()
            .ToDictionary(m => m.GetProperty("name").GetString()!, m => m.GetProperty("value").GetDouble());
        var work = await page.EvaluateAsync<JsonElement>(Work);
        // Load it once more and keep the shorter parse time: parsing waits for blocking scripts, and one
        // slow download through the proxy can inflate a single load at any rate.
        await page.ReloadAsync(new() { WaitUntil = WaitUntilState.Load, Timeout = 90000 });
        var again = await page.EvaluateAsync<JsonElement>(Nav);
        var parseAgain = again.GetProperty("dcl_ms").GetInt64() - again.GetProperty("response_ms").GetInt64();
        var responseMs = nav.GetProperty("response_ms").GetInt64();
        var dclMs = nav.GetProperty("dcl_ms").GetInt64();
        rows.Add(new JsonObject
        {
            ["rate"] = rate,
            ["work_ms"] = work.GetProperty("ms").GetInt64(),
            ["response_ms"] = responseMs,
            ["dcl_ms"] = dclMs,
            ["load_ms"] = nav.GetProperty("load_ms").GetInt64(),
            ["parse_ms"] = Math.Min(dclMs - responseMs, parseAgain), // HTML fully received → DOMContentLoaded: parsing and scripts
            ["main_thread_ms"] = Math.Round(metrics["TaskDuration"] * 1000), // busy time while loading, from CDP
            ["check"] = work.GetProperty("check").GetInt64(),
        });
        await context.CloseAsync();
    }

    // Is it per page? A throttled tab next to an unthrottled one in the same browser.
    var sideContext = await browser.NewContextAsync();
    var slow = await sideContext.NewPageAsync();
    var fast = await sideContext.NewPageAsync();
    await (await sideContext.NewCDPSessionAsync(slow)).SendAsync("Emulation.setCPUThrottlingRate", new() { ["rate"] = 6 });
    var sideBySide = new JsonObject
    {
        ["throttled_tab_ms"] = (await slow.EvaluateAsync<JsonElement>(Work)).GetProperty("ms").GetInt64(),
        ["other_tab_ms"] = (await fast.EvaluateAsync<JsonElement>(Work)).GetProperty("ms").GetInt64(),
    };
    await sideContext.CloseAsync();

    var result = new JsonObject { ["rows"] = rows, ["side_by_side"] = sideBySide };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
