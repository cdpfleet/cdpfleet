// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, COMMUNITY_API_KEY (a Community account: 2 threads, to reach the cap)
// There is no published cdpfleet SDK for .NET yet: this is the plain version, with the retry written by hand.
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var freeKey = Environment.GetEnvironmentVariable("COMMUNITY_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
const string Starter = "https://starter.cdpfleet.com";
const string Api = "https://cdpfleet.com";
var http = new HttpClient();
string[] urls = new[] { 1, 2, 3 }.Select(n => $"https://books.toscrape.com/catalogue/page-{n}.html").ToArray();

// The scrape every variant runs: three catalogue pages, every book title.
async Task<JsonObject> Scrape(IBrowser browser, Stopwatch sw, long connectedMs)
{
    var page = await browser.NewPageAsync();
    var titles = new List<string>();
    foreach (var url in urls)
    {
        // A shared proxy drops the odd connection: one retry.
        try { await page.GotoAsync(url, new() { Timeout = 45000 }); }
        catch (PlaywrightException) { await page.GotoAsync(url, new() { Timeout = 45000 }); }
        titles.AddRange(await page.Locator("article.product_pod h3 a").EvaluateAllAsync<string[]>("as => as.map(a => a.getAttribute('title'))"));
    }
    return new JsonObject { ["books"] = titles.Count, ["first_title"] = titles[0], ["connected_ms"] = connectedMs, ["seconds"] = sw.Elapsed.TotalSeconds };
}

// ---- 1a. Plain Playwright: launch over HTTP yourself, then connect with the key header.
async Task<(int Status, string? RetryAfter, JsonObject? Body)> Launch(string k, object body)
{
    using var req = new HttpRequestMessage(HttpMethod.Post, $"{Starter}/chromium/session") { Content = JsonContent.Create(body) };
    req.Headers.Add("x-api-key", k);
    using var res = await http.SendAsync(req);
    JsonObject? data = null;
    try { data = JsonNode.Parse(await res.Content.ReadAsStringAsync())?.AsObject(); } catch (JsonException) { }
    return ((int)res.StatusCode, res.Headers.RetryAfter?.Delta?.TotalSeconds.ToString(), data);
}

async Task<JsonObject> Plain(IPlaywright pw)
{
    var sw = Stopwatch.StartNew();
    var r = await Launch(key, new { proxy, headless = true, record = false });
    if (r.Status != 200) throw new Exception($"launch {r.Status} {r.Body}");
    var browser = await pw.Chromium.ConnectAsync(r.Body!["wsUrl"]!.GetValue<string>(), new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try { return await Scrape(browser, sw, sw.ElapsedMilliseconds); }
    finally { await browser.CloseAsync(); } // closing the connection ends the session
}

// ---- 1c. A connect URL built by hand: one Connect() launches the browser.
async Task<JsonObject> ConnectUrl(IPlaywright pw)
{
    var sw = Stopwatch.StartNew();
    var url = $"wss://router01.cdpfleet.com/connect/chromium?proxy={Uri.EscapeDataString(proxy)}&headless=true&record=false";
    var browser = await pw.Chromium.ConnectAsync(url, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try { return await Scrape(browser, sw, sw.ElapsedMilliseconds); }
    finally { await browser.CloseAsync(); }
}

// ---- 2. At the thread cap (Community: 2 threads). Hold both, then ask for a third.
async Task EndSession(string k, string id)
{
    using var req = new HttpRequestMessage(HttpMethod.Delete, $"{Api}/v1/me/threads/{id}");
    req.Headers.Add("x-api-key", k);
    try { (await http.SendAsync(req)).Dispose(); } catch (HttpRequestException) { }
}

async Task<int> LiveThreads(string k)
{
    using var req = new HttpRequestMessage(HttpMethod.Get, $"{Api}/v1/me/threads");
    req.Headers.Add("x-api-key", k);
    using var res = await http.SendAsync(req);
    return JsonNode.Parse(await res.Content.ReadAsStringAsync())!["threads"]!.AsArray().Count;
}

// What the SDKs do for you: retry 429 (not the daily/hourly quota) and 503, waiting Retry-After, up to 120 s.
async Task<JsonObject> LaunchWithRetry(string k, object body)
{
    var deadline = DateTime.UtcNow.AddSeconds(120);
    for (var attempt = 1; ; attempt++)
    {
        var r = await Launch(k, body);
        if (r.Status == 200) return r.Body!;
        var code = r.Body?["error"]?.GetValue<string>() ?? "";
        var quota = code is "daily_quota_exhausted" or "hourly_quota_exhausted";
        var retryable = r.Status == 503 || (r.Status == 429 && !quota);
        if (!retryable || DateTime.UtcNow > deadline) throw new Exception($"launch {r.Status} {code}");
        var after = double.TryParse(r.RetryAfter, out var s) && s > 0 ? s : Math.Min(Math.Pow(2, attempt - 1), 10);
        await Task.Delay(TimeSpan.FromSeconds(after + Random.Shared.NextDouble() * 0.5));
    }
}

async Task<JsonObject> AtTheCap()
{
    var body = new { headless = true, record = false };
    var held = new List<string>();
    for (var i = 0; i < 2; i++)
    {
        var r = await Launch(freeKey, body);
        if (r.Status != 200) throw new Exception($"hold {r.Status} {r.Body}");
        held.Add(r.Body!["sessionId"]!.GetValue<string>());
        await Task.Delay(1500); // Community launches: at most 2 a second, 12 a minute
    }
    // Plain: the answer comes back at once and the retry is yours to write.
    var third = await Launch(freeKey, body);
    await Task.Delay(1500);
    // The hand-written loop: free a thread 5 s later and see when it returns.
    var first = held[0];
    held.RemoveAt(0);
    var release = Task.Delay(5000).ContinueWith(_ => EndSession(freeKey, first)).Unwrap();
    var sw = Stopwatch.StartNew();
    JsonObject retry;
    try
    {
        var s = await LaunchWithRetry(freeKey, body);
        retry = new JsonObject { ["by"] = "hand-written", ["got_session"] = true, ["waited_s"] = sw.Elapsed.TotalSeconds };
        await EndSession(freeKey, s["sessionId"]!.GetValue<string>()); // never connected: end it explicitly
    }
    catch (Exception e)
    {
        retry = new JsonObject { ["by"] = "hand-written", ["got_session"] = false, ["waited_s"] = sw.Elapsed.TotalSeconds, ["error"] = e.Message };
    }
    await release;
    foreach (var id in held) await EndSession(freeKey, id);
    await Task.Delay(3000);
    return new JsonObject
    {
        ["threads"] = 2,
        ["plain"] = new JsonObject
        {
            ["status"] = third.Status,
            ["error"] = third.Body?["error"]?.GetValue<string>(),
            ["retry_after"] = third.RetryAfter,
            ["in_use"] = third.Body?["in_use"]?.GetValue<int>(),
            ["upgrade_url"] = third.Body?["upgrade"]?["url"]?.GetValue<string>(),
        },
        ["retry"] = retry,
        ["threads_left"] = await LiveThreads(freeKey),
    };
}

using var playwright = await Playwright.CreateAsync();
var output = new JsonObject
{
    ["language"] = "csharp",
    ["sdk"] = null,
    ["scrape"] = new JsonObject { ["plain"] = await Plain(playwright), ["sdk"] = null, ["connect_url"] = await ConnectUrl(playwright) },
    ["cap"] = await AtTheCap(),
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
