// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (http://user:pass@host:port)
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

// One browser per engine family, and the Playwright client that speaks to it.
var browsers = new (string Name, IBrowserType Family)[]
{
    ("chrome", playwright.Chromium), ("edge", playwright.Chromium), ("firefox", playwright.Firefox),
    ("camoufox", playwright.Firefox), ("webkit", playwright.Webkit),
};

// A residential exit occasionally times out: one retry, in a fresh tab.
async Task<JsonNode> Fingerprint(IBrowser browser)
{
    for (var attempt = 1; ; attempt++)
    {
        try
        {
            var page = await browser.NewPageAsync();
            return JsonNode.Parse(await (await page.GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 30000 }))!.TextAsync())!;
        }
        catch (Exception err) when ((err is PlaywrightException or TimeoutException) && attempt < 2) { }
    }
}

var rows = new JsonArray();
foreach (var (name, family) in browsers)
{
    var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{name}/session", new { proxy, headless = true });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {name}: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await family.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        // Navigate the browser itself: the JSON describes the TLS ClientHello and HTTP/2
        // frames this very browser sent (APIRequest would use Playwright's own client).
        var fp = await Fingerprint(browser);
        var tls = fp["tls"]!;
        var h2 = fp["http2"] as JsonObject;
        rows.Add(new JsonObject
        {
            ["browser"] = name,
            ["version"] = browser.Version,
            ["user_agent"] = fp["user_agent"]?.DeepClone(),
            ["http_version"] = fp["http_version"]?.DeepClone(),
            ["ja4"] = tls["ja4"]?.DeepClone(),
            ["ja3_hash"] = tls["ja3_hash"]?.DeepClone(),
            ["peetprint_hash"] = tls["peetprint_hash"]?.DeepClone(),
            ["akamai_h2"] = h2?["akamai_fingerprint"]?.DeepClone(),
            ["akamai_h2_hash"] = h2?["akamai_fingerprint_hash"]?.DeepClone(),
            ["cipher_suites"] = tls["ciphers"]!.AsArray().Count,
            ["extensions"] = tls["extensions"]!.AsArray().Count,
        });
    }
    finally
    {
        await browser.CloseAsync(); // ends the session and stops billing
    }
}
Console.WriteLine(rows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
