// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var pages = new (string Name, string Url)[]
{
    ("example.com", "https://example.com/"),
    ("books.toscrape.com", "https://books.toscrape.com/"),
    ("Wikipedia article", "https://en.wikipedia.org/wiki/Web_browser"),
    ("shifting demo", "https://shift.test/"), // served by page.route below
};
// A made-up page whose banner arrives late and pushes the text down: a layout shift on purpose.
var shifting = "<!doctype html><meta charset=\"utf-8\"><title>shift</title>\n"
    + "<body style=\"margin:0;font:18px sans-serif\"><div id=\"slot\"></div>\n<h1>Article</h1>"
    + string.Concat(Enumerable.Repeat("<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>", 30))
    + "\n<script>setTimeout(() => { document.getElementById('slot').innerHTML = '<div style=\"height:300px;background:#fc0\">Late banner</div>'; }, 600);</script>";

// Reads the vitals from the page's own performance timeline. `buffered: true` returns the
// entries recorded before the observer existed, so this runs after the load.
const string Vitals = """
new Promise((resolve) => {
  const nav = performance.getEntriesByType('navigation')[0];
  const fcp = performance.getEntriesByName('first-contentful-paint')[0];
  let lcp = null, cls = 0;
  new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e; }).observe({ type: 'largest-contentful-paint', buffered: true });
  new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  setTimeout(() => resolve({
    ttfb_ms: Math.round(nav.responseStart),
    connect_ms: Math.round(nav.connectEnd - nav.domainLookupStart),
    wait_ms: Math.round(nav.responseStart - nav.requestStart),
    fcp_ms: fcp ? Math.round(fcp.startTime) : null,
    lcp_ms: lcp ? Math.round(lcp.startTime) : null,
    lcp_element: lcp && lcp.element ? lcp.element.tagName.toLowerCase() : null,
    cls: Math.round(cls * 1000) / 1000,
    load_ms: Math.round(nav.loadEventEnd),
  }), 200);
})
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
    foreach (var (name, url) in pages)
    {
        var context = await browser.NewContextAsync(new() { ViewportSize = new() { Width = 1280, Height = 800 } }); // cold cache
        await context.RouteAsync("https://shift.test/**", route => route.FulfillAsync(new() { ContentType = "text/html; charset=utf-8", Body = shifting }));
        var page = await context.NewPageAsync();
        // A CDP session over the normal Playwright connection: the browser's own counters.
        var cdp = await context.NewCDPSessionAsync(page);
        await cdp.SendAsync("Performance.enable");
        await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 });
        await page.WaitForTimeoutAsync(1500); // let late content (and late shifts) land
        var vitals = await page.EvaluateAsync<JsonElement>(Vitals);
        var row = new JsonObject { ["page"] = name };
        foreach (var p in vitals.EnumerateObject()) row[p.Name] = JsonNode.Parse(p.Value.GetRawText());
        var metrics = (await cdp.SendAsync("Performance.getMetrics"))!.Value.GetProperty("metrics").EnumerateArray()
            .ToDictionary(m => m.GetProperty("name").GetString()!, m => m.GetProperty("value").GetDouble());
        row["script_ms"] = Math.Round(metrics["ScriptDuration"] * 1000);
        row["layouts"] = (long)metrics["LayoutCount"];
        row["dom_nodes"] = (long)metrics["Nodes"];
        row["js_heap_mb"] = Math.Round(metrics["JSHeapUsedSize"] / 1048576, 1);
        rows.Add(row);
        await context.CloseAsync();
    }
    var result = new JsonObject { ["rows"] = rows };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
