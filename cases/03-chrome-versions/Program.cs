// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// The live catalog says which channels and previous majors exist right now.
var catalog = JsonNode.Parse(await http.GetStringAsync("https://cdpfleet.com/api/public/browsers"))!;
var versions = catalog["engines"]!.AsArray().First(e => (string?)e!["key"] == "chrome")!["versions"]!.AsArray();
var variants = versions.Select(v =>
{
    var label = (string)v!["label"]!;
    var version = (string)v["version"]!;
    var options = new JsonObject { ["proxy"] = proxy, ["headless"] = false }; // a real display: no "HeadlessChrome"
    if (label == "pinned") { options["version"] = version.Split('.')[0]; return ($"version {version.Split('.')[0]}", options, version); }
    if (label != "stable") options["channel"] = label;
    return ($"channel {label}", options, version);
}).ToList();

async Task<JsonObject> Probe((string Label, JsonObject Options, string Expected) v)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chrome/session", v.Options);
    if (!res.IsSuccessStatusCode) return new JsonObject { ["variant"] = v.Label, ["error"] = $"{(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        IResponse? nav;
        try { nav = await (await browser.NewPageAsync()).GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 30000 }); }
        catch (Exception err) when (err is PlaywrightException or TimeoutException) // a residential exit occasionally times out: one retry, in a fresh tab
        { nav = await (await browser.NewPageAsync()).GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 30000 }); }
        var fp = JsonNode.Parse(await nav!.TextAsync())!;
        var headers = fp["http2"]!["sent_frames"]!.AsArray().First(f => (string?)f!["frame_type"] == "HEADERS")!["headers"]!.AsArray();
        return new JsonObject
        {
            ["variant"] = v.Label,
            ["catalog_version"] = v.Expected,
            ["browser_version"] = browser.Version,
            ["user_agent"] = fp["user_agent"]!.DeepClone(),
            ["sec_ch_ua"] = headers.Select(h => (string)h!).FirstOrDefault(h => h.StartsWith("sec-ch-ua: "))?[11..],
            ["ja4"] = fp["tls"]!["ja4"]!.DeepClone(),
            ["akamai_h2_hash"] = fp["http2"]!["akamai_fingerprint_hash"]!.DeepClone(),
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

// All variants at once: each is its own session.
var rows = new JsonArray((await Task.WhenAll(variants.Select(Probe))).ToArray<JsonNode?>());
Console.WriteLine(rows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
