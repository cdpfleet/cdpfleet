// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chrome/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = false });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// What the server saw: header names in order, and the HTTP/2 fingerprint.
async Task<JsonObject> Observe(string label, BrowserNewContextOptions options, Func<IPage, Task>? setup = null)
{
    var context = await browser.NewContextAsync(options);
    var page = await context.NewPageAsync();
    if (setup != null) await setup(page);
    var fp = JsonNode.Parse(await (await page.GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 60000 }))!.TextAsync())!;
    await context.CloseAsync();
    var headers = fp["http2"]!["sent_frames"]!.AsArray().First(f => (string?)f!["frame_type"] == "HEADERS")!["headers"]!.AsArray()
        .Select(h => (string)h!).ToList();
    return new JsonObject
    {
        ["variant"] = label,
        ["header_order"] = new JsonArray(headers.Select(h => (JsonNode?)h[..h.IndexOf(':', 1)]).ToArray()),
        ["accept_language"] = headers.FirstOrDefault(h => h.StartsWith("accept-language: "))?[17..],
        ["akamai_h2"] = fp["http2"]!["akamai_fingerprint"]!.DeepClone(),
        ["ja4"] = fp["tls"]!["ja4"]!.DeepClone(),
    };
}

try
{
    var rows = new JsonArray
    {
        await Observe("default", new()),
        await Observe("locale: de-DE", new() { Locale = "de-DE" }),
        await Observe("extraHTTPHeaders", new() { ExtraHTTPHeaders = new Dictionary<string, string> { ["accept-language"] = "de-DE", ["x-request-id"] = "abc123" } }),
        await Observe("route: rewrite headers", new(), page => page.RouteAsync("**/*", route =>
        {
            var headers = new Dictionary<string, string>(route.Request.Headers) { ["x-request-id"] = "abc123" };
            return route.ContinueAsync(new() { Headers = headers });
        })),
    };
    Console.WriteLine(rows.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
