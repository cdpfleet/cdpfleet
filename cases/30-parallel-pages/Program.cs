// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

string[] urls = [
    "https://books.toscrape.com/",
    "https://quotes.toscrape.com/",
    "https://example.com",
    "https://en.wikipedia.org/wiki/Web_scraping",
    "https://news.ycombinator.com/",
];

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
    async Task<JsonObject> Extract(string url)
    {
        // One retry, in a fresh tab: over 30 loads through a rotating proxy, one occasionally fails.
        for (var attempt = 1; ; attempt++)
        {
            var page = await browser.NewPageAsync();
            try
            {
                await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 60000 });
                return new JsonObject { ["url"] = url, ["title"] = await page.TitleAsync() };
            }
            catch (PlaywrightException) when (attempt == 1)
            {
            }
            finally
            {
                await page.CloseAsync();
            }
        }
    }

    // Three rounds of each, alternating; the best time of each method counts. One page that
    // stalls for 20 s in the proxy would otherwise decide a single comparison on its own.
    var rounds = new JsonArray();
    var sequential = new List<JsonObject>();
    JsonObject[] parallelResults = [];
    double seqSeconds = double.MaxValue, parSeconds = double.MaxValue;
    for (var round = 0; round < 3; round++)
    {
        var sw1 = Stopwatch.StartNew();
        sequential = new List<JsonObject>();
        foreach (var url in urls)
        {
            var r = await Extract(url);
            r["method"] = "sequential";
            sequential.Add(r);
        }
        var seq = Math.Round(sw1.Elapsed.TotalSeconds, 2);

        var sw2 = Stopwatch.StartNew();
        var tasks = urls.Select(async url =>
        {
            var r = await Extract(url);
            r["method"] = "parallel";
            return r;
        }).ToArray();
        parallelResults = await Task.WhenAll(tasks);
        var par = Math.Round(sw2.Elapsed.TotalSeconds, 2);
        rounds.Add(new JsonObject { ["sequential_seconds"] = seq, ["parallel_seconds"] = par });
        seqSeconds = Math.Min(seqSeconds, seq);
        parSeconds = Math.Min(parSeconds, par);
    }

    var results = new JsonArray();
    foreach (var r in sequential) results.Add(r);
    foreach (var r in parallelResults) results.Add(r);

    var output = new JsonObject
    {
        ["sequential_seconds"] = seqSeconds,
        ["parallel_seconds"] = parSeconds,
        ["speedup"] = $"{Math.Round(seqSeconds / parSeconds, 1)}x",
        ["rounds"] = rounds,
        ["results"] = results,
    };
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
