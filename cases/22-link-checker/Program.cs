// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Start = "https://books.toscrape.com/"; // 70-odd same-site links on the front page
const int Wave = 8; // links checked at once
const string LinksJs = "(as, origin) => [...new Set(as.map(a => a.href))].filter(h => h.startsWith(origin) && !h.includes('#'))";
const string FetchJs = @"urls => Promise.all(urls.map(async url => {
  try { const r = await fetch(url, { cache: 'no-store' }); return { url, status: r.status, redirected: r.redirected }; }
  catch { return { url, status: 0, redirected: false }; }
}))";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

static JsonObject Summary(string method, List<(string Url, int Status, bool Redirected)> results, double seconds, int requestsMade)
{
    var broken = results.Where(r => r.Status == 0 || r.Status >= 400).ToList();
    return new JsonObject
    {
        ["method"] = method,
        ["links"] = results.Count,
        ["ok"] = results.Count(r => r.Status >= 200 && r.Status < 300),
        ["redirected"] = results.Count(r => r.Redirected),
        ["broken"] = broken.Count,
        ["broken_urls"] = new JsonArray(broken.Take(5).Select(r => (JsonNode)r.Url).ToArray()),
        ["seconds"] = Math.Round(seconds, 3),
        ["seconds_per_link"] = Math.Round(seconds / results.Count, 2),
        ["requests_made"] = requestsMade,
    };
}

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync();
    await page.GotoAsync(Start, new() { Timeout = 60000 });
    // Every unique same-site link on the page, as absolute URLs.
    var origin = new Uri(Start).GetLeftPart(UriPartial.Authority);
    var links = (await page.EvalOnSelectorAllAsync<JsonElement>("a[href]", LinksJs, origin)).EnumerateArray().Select(e => e.GetString()!).ToList();

    // Way 1: fetch() inside the page, Wave links at a time — the browser's proxy, cookies
    // and TLS, no navigation, no rendering, no assets.
    var sw1 = Stopwatch.StartNew();
    var fetched = new List<(string, int, bool)>();
    for (int i = 0; i < links.Count; i += Wave)
    {
        var wave = links.Skip(i).Take(Wave).ToArray();
        var results = await page.EvaluateAsync<JsonElement>(FetchJs, wave);
        foreach (var r in results.EnumerateArray())
            fetched.Add((r.GetProperty("url").GetString()!, r.GetProperty("status").GetInt32(), r.GetProperty("redirected").GetBoolean()));
    }
    var inPage = Summary($"fetch() in the page, {Wave} at a time", fetched, sw1.Elapsed.TotalSeconds, links.Count);

    // Way 2: navigate to each link, like a user — full page loads with all their assets.
    // Only a sample: this is the slow way, and it is the same work for every link.
    var sample = links.Take(10).ToList();
    int requests = 0;
    page.Request += (_, _) => Interlocked.Increment(ref requests);
    var sw2 = Stopwatch.StartNew();
    var navigated = new List<(string, int, bool)>();
    foreach (var url in sample)
    {
        try
        {
            var r = await page.GotoAsync(url, new() { Timeout = 60000 });
            navigated.Add((url, r?.Status ?? 0, r != null && r.Url != url));
        }
        catch (Exception e) when (e is PlaywrightException or TimeoutException)
        {
            navigated.Add((url, 0, false));
        }
    }
    var byNav = Summary("page.goto each link (10-link sample)", navigated, sw2.Elapsed.TotalSeconds, requests);

    var outRows = new JsonArray { inPage, byNav };
    Console.WriteLine(outRows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
