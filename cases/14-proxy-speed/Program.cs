// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string PageUrl = "https://en.wikipedia.org/wiki/Web_browser";
const int Loads = 3; // per proxy, each in a fresh context (cold cache, new connections)
var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxies = new (string Label, string Proxy)[]
{
    ("residential (any country)", Environment.GetEnvironmentVariable("PROXY_URL")!),
    ("residential (Germany)", Environment.GetEnvironmentVariable("PROXY_URL_DE")!),
    ("datacenter SOCKS5", Environment.GetEnvironmentVariable("SOCKS_PROXIES")!.Split(',')[0]),
};

// Navigation Timing + Largest Contentful Paint, read in the page after load.
const string Timings = """
    () => new Promise((done) => {
      const nav = performance.getEntriesByType('navigation')[0];
      new PerformanceObserver((list) => {
        const lcp = list.getEntries().at(-1);
        done({
          connect: nav.connectEnd - nav.connectStart,
          ttfb: nav.responseStart - nav.requestStart,
          domReady: nav.domContentLoadedEventEnd,
          load: nav.loadEventEnd,
          lcp: lcp.startTime,
        });
      }).observe({ type: 'largest-contentful-paint', buffered: true });
    })
    """;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> Measure(string label, string proxy)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", new { proxy, headless = true });
    if (!res.IsSuccessStatusCode) return new JsonObject { ["proxy"] = label, ["error"] = $"launch {(int)res.StatusCode}" };
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var runs = new List<JsonElement>();
        var failures = 0;
        while (runs.Count < Loads)
        {
            var context = await browser.NewContextAsync();
            try
            {
                var page = await context.NewPageAsync();
                await page.GotoAsync(PageUrl, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 });
                runs.Add(await page.EvaluateAsync<JsonElement>(Timings));
            }
            catch (Exception err) when ((err is PlaywrightException or TimeoutException) && ++failures <= 1) { } // one failed load is the proxy's noise
            finally { await context.CloseAsync(); }
        }
        long Median(string k) => (long)Math.Round(runs.Select(r => r.GetProperty(k).GetDouble()).OrderBy(v => v).ElementAt(runs.Count / 2));
        return new JsonObject
        {
            ["proxy"] = label, ["loads"] = runs.Count, ["connect_ms"] = Median("connect"), ["ttfb_ms"] = Median("ttfb"),
            ["dom_ready_ms"] = Median("domReady"), ["lcp_ms"] = Median("lcp"), ["load_ms"] = Median("load"),
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var rows = await Task.WhenAll(proxies.Select(p => Measure(p.Label, p.Proxy)));
var result = new JsonObject { ["page"] = PageUrl, ["statistic"] = $"median of {Loads} cold loads", ["results"] = new JsonArray(rows.ToArray<JsonNode?>()) };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
