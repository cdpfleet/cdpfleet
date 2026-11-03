// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (your plan), PROXY_URL; optional COMMUNITY_API_KEY, CDPFLEET_DEDICATED_API_KEY to compare plans
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Starter = "https://starter.cdpfleet.com";
const string Api = "https://cdpfleet.com";
const string PageUrl = "https://en.wikipedia.org/wiki/World_War_II"; // a long article: a mid-weight page
const int MaxTabs = 50; // the per-session tab cap on every plan
var proxy = Environment.GetEnvironmentVariable("PROXY_URL");
var http = new HttpClient();

// Each run: an account, an engine and a memory multiplier. Runs whose key isn't set are skipped.
var runs = new (string Env, string Engine, int Memory)[]
{
    ("COMMUNITY_API_KEY", "chromium", 1),
    ("CDPFLEET_API_KEY", "chromium", 1),
    ("CDPFLEET_API_KEY", "firefox", 1),
    ("CDPFLEET_API_KEY", "webkit", 1),
    ("CDPFLEET_API_KEY", "chromium", 2),
    ("CDPFLEET_DEDICATED_API_KEY", "chromium", 1),
}.Where(r => Environment.GetEnvironmentVariable(r.Env) != null).ToArray();

async Task<JsonObject> ApiGet(string key, string path)
{
    using var req = new HttpRequestMessage(HttpMethod.Get, $"{Api}{path}");
    req.Headers.Add("x-api-key", key);
    using var res = await http.SendAsync(req);
    return JsonNode.Parse(await res.Content.ReadAsStringAsync())!.AsObject();
}
string FirstLine(Exception e) => e.Message.Split('\n')[0];

async Task<JsonObject> Run(IPlaywright pw, string env, string engine, int memory)
{
    var key = Environment.GetEnvironmentVariable(env)!;
    var sub = (await ApiGet(key, "/v1/me"))["subscription"]!.AsObject();
    var community = sub["features"] is JsonObject f && f["community"] is JsonNode c && (bool)c;
    var plan = community ? "Community" : (string?)sub["kind"] == "dedicated" ? "Dedicated" : "Shared";
    var t0 = DateTime.UtcNow;
    // Community launches use the plan's included proxy; paid plans bring their own.
    var body = new JsonObject { ["headless"] = true };
    if (memory > 1) body["memory"] = memory;
    if (!community) body["proxy"] = proxy;
    using var req = new HttpRequestMessage(HttpMethod.Post, $"{Starter}/{engine}/session") { Content = JsonContent.Create(body) };
    req.Headers.Add("x-api-key", key);
    using var res = await http.SendAsync(req);
    var output = new JsonObject { ["plan"] = plan, ["engine"] = engine, ["memory"] = memory };
    if (!res.IsSuccessStatusCode) { output["launch_error"] = $"{(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}"; return output; }
    var launch = JsonNode.Parse(await res.Content.ReadAsStringAsync())!.AsObject();
    var type = engine == "firefox" ? pw.Firefox : engine == "webkit" ? pw.Webkit : pw.Chromium;
    var browser = await type.ConnectAsync((string)launch["wsUrl"]!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var context = await browser.NewContextAsync();
    // Open the article in one new tab after another until the session ends or the tab cap is reached.
    var tabs = 0;
    string? clientError = null;
    while (tabs < MaxTabs)
    {
        try
        {
            var page = await context.NewPageAsync();
            // A slow load through a shared proxy gets one more try; a closed session doesn't.
            try { await page.GotoAsync(PageUrl, new() { Timeout = 60000 }); }
            catch (TimeoutException) { await page.GotoAsync(PageUrl, new() { Timeout = 60000 }); }
            tabs++;
        }
        catch (Exception e) { clientError = FirstLine(e); break; }
    }
    // At the cap with memory to spare: what does a 51st tab get?
    string? tab51 = null;
    if (tabs == MaxTabs)
    {
        try { await context.NewPageAsync(); tab51 = "opened"; }
        catch (Exception e) { tab51 = FirstLine(e); }
    }
    try { await browser.CloseAsync(); } catch { }
    // The session's record: why it ended, its weight, peak memory and time.
    JsonObject? record = null;
    for (var i = 0; i < 20 && record?["ended_at"] is null; i++)
    {
        await Task.Delay(1500);
        record = (await ApiGet(key, "/v1/me/sessions?limit=20"))["sessions"]!.AsArray().Select(x => x!.AsObject())
            .FirstOrDefault(x => (string?)x["session_id"] == (string?)launch["sessionId"]);
    }
    if (clientError != null)
    {
        var m = Regex.Match(clientError, "(Session closed: .*)$");
        if (m.Success) clientError = m.Groups[1].Value;
    }
    output["weight"] = launch["weight"]?.DeepClone();
    output["memory_mb"] = launch["memory_mb"]?.DeepClone();
    output["tabs"] = tabs;
    output["client_error"] = clientError;
    output["tab_51"] = tab51;
    output["end_reason"] = record?["end_reason"]?.DeepClone();
    output["memory_peak_mb"] = record?["memory_peak_mb"]?.DeepClone();
    output["seconds"] = record != null ? (int)record["duration_seconds"]! : (int)(DateTime.UtcNow - t0).TotalSeconds;
    output["thread_seconds"] = record != null ? (int)record["duration_seconds"]! * (int)record["weight"]! : null;
    return output;
}

using var playwright = await Playwright.CreateAsync();
var results = new JsonArray();
foreach (var r in runs) results.Add(await Run(playwright, r.Env, r.Engine, r.Memory)); // one at a time: one site, one session at a time
var doc = new JsonObject
{
    ["language"] = "csharp",
    ["page"] = PageUrl,
    ["max_tabs"] = MaxTabs,
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
    ["runs"] = results,
};
Console.WriteLine(doc.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
