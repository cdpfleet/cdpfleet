// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Web;
using Microsoft.Playwright;

const string Start = "https://quotes.toscrape.com/scroll"; // loads 10 quotes per screen, 100 in all
const string QuotesJs = "els => els.map(e => ({ text: e.querySelector('.text').textContent, author: e.querySelector('.author').textContent }))";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

// Way 1: behave like a user — scroll to the bottom until nothing more appears, then read the DOM.
static async Task<JsonObject> ByScrolling(IPage page)
{
    var sw = Stopwatch.StartNew();
    int requests = 0;
    page.Request += (_, _) => Interlocked.Increment(ref requests);
    await page.GotoAsync(Start, new() { Timeout = 60000 });
    await page.Locator(".quote").First.WaitForAsync(new() { Timeout = 60000 });
    var loaded = sw.ElapsedMilliseconds;
    int count = 0, scrolls = 0, stale = 0;
    while (stale < 3)
    {
        await page.EvaluateAsync("window.scrollTo(0, document.body.scrollHeight)");
        scrolls++;
        await page.WaitForTimeoutAsync(600);
        var now = await page.Locator(".quote").CountAsync();
        stale = now > count ? 0 : stale + 1;
        count = now;
    }
    var quotes = await page.EvalOnSelectorAllAsync<JsonElement>(".quote", QuotesJs);
    var done = sw.ElapsedMilliseconds;
    var authors = quotes.EnumerateArray().Select(q => q.GetProperty("author").GetString()).ToHashSet();
    return new JsonObject
    {
        ["method"] = "scroll the page", ["quotes"] = quotes.GetArrayLength(), ["authors"] = authors.Count,
        ["scrolls"] = scrolls, ["requests"] = requests,
        ["load_seconds"] = loaded / 1000.0, ["collect_seconds"] = (done - loaded) / 1000.0,
    };
}

// The endpoint's URL with its page parameter replaced.
static string WithPage(Uri endpoint, int n)
{
    var query = HttpUtility.ParseQueryString(endpoint.Query);
    query["page"] = n.ToString();
    return $"{endpoint.GetLeftPart(UriPartial.Path)}?{query}";
}

// Way 2: catch the JSON request the page makes for its first screen, then call that endpoint
// yourself from inside the browser (same proxy, cookies and TLS fingerprint as the page),
// several pages at a time. No scrolling, no guessing when loading has finished.
static async Task<JsonObject> ByApi(IPage page)
{
    var sw = Stopwatch.StartNew();
    int requests = 0;
    page.Request += (_, _) => Interlocked.Increment(ref requests);
    var response = await page.RunAndWaitForResponseAsync(
        async () => await page.GotoAsync(Start, new() { Timeout = 60000 }),
        r => r.Url.Contains("/api/") && r.Request.ResourceType == "xhr",
        new() { Timeout = 60000 });
    var loaded = sw.ElapsedMilliseconds;
    var endpoint = new Uri(response.Url);
    var quotes = (await response.JsonAsync())!.Value.GetProperty("quotes").EnumerateArray().ToList(); // page 1 came for free
    const int wave = 4; // one round trip through the proxy per wave, not per page
    int next = 2;
    for (bool more = true; more; next += wave)
    {
        var urls = Enumerable.Range(next, wave).Select(n => WithPage(endpoint, n)).ToArray();
        var pages = await page.EvaluateAsync<JsonElement>("us => Promise.all(us.map(u => fetch(u).then(r => r.json())))", urls);
        more = true;
        foreach (var p in pages.EnumerateArray())
        {
            quotes.AddRange(p.GetProperty("quotes").EnumerateArray());
            more &= p.GetProperty("has_next").GetBoolean();
        }
    }
    var done = sw.ElapsedMilliseconds;
    var authors = quotes.Select(q => q.GetProperty("author").GetProperty("name").GetString()).ToHashSet();
    return new JsonObject
    {
        ["method"] = "call its JSON API", ["quotes"] = quotes.Count, ["authors"] = authors.Count,
        ["api_pages"] = next - 1, ["endpoint"] = $"{endpoint.AbsolutePath}?page=N", ["requests"] = requests,
        ["load_seconds"] = loaded / 1000.0, ["collect_seconds"] = (done - loaded) / 1000.0,
    };
}

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var outRows = new JsonArray();
    foreach (var way in new Func<IPage, Task<JsonObject>>[] { ByScrolling, ByApi })
    {
        var page = await browser.NewPageAsync(); // a fresh tab per method, so request counts don't mix
        outRows.Add(await way(page));
        await page.CloseAsync();
    }
    Console.WriteLine(outRows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
