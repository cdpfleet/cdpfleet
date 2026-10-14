// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Site = "https://books.toscrape.com/";
var setups = new (string Label, string Engine, string Family, string Api)[]
{
    ("Chrome", "chrome", "chromium", "typeof chrome !== 'undefined' && !!chrome.runtime"),
    ("Firefox", "firefox", "firefox", "typeof browser !== 'undefined' && !!browser.runtime"),
};

// What an extension's content script does: runs in every page before the page's own scripts.
const string ContentScript = """
  window.__myExtension = true;
  document.addEventListener('DOMContentLoaded', () => {
    const b = document.createElement('div');
    b.id = 'my-extension-banner';
    b.textContent = 'Injected on ' + location.pathname;
    document.body.prepend(b);
  });
""";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<(int Status, JsonElement Session)> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return ((int)res.StatusCode, await res.Content.ReadFromJsonAsync<JsonElement>());
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {engine} {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// Try to load an extension the way a local launch would: Chromium flags and an "extensions" list.
// The fleet refuses browser-process fields with 400 unsupported_option, so nothing is started.
async Task<(int Status, string? Error)> TryExtensions(string engine)
{
    var body = new JsonObject
    {
        ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"),
        ["args"] = new JsonArray("--load-extension=/tmp/my-extension", "--user-agent=ARGS-WERE-APPLIED"),
        ["extensions"] = new JsonArray("/tmp/my-extension"),
    };
    var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
        new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
    string? error = null;
    try
    {
        var json = await res.Content.ReadFromJsonAsync<JsonElement>();
        if (json.ValueKind == JsonValueKind.Object && json.TryGetProperty("error", out var e) && e.ValueKind == JsonValueKind.String) error = e.GetString();
    }
    catch (JsonException) { }
    return ((int)res.StatusCode, error);
}

async Task<JsonObject> One(string label, string engine, string family, string api)
{
    var (refusedStatus, refusedError) = await TryExtensions(engine);
    var outp = new JsonObject { ["label"] = label, ["launch_status"] = refusedStatus, ["launch_error"] = refusedError };
    // A normal launch, for the Playwright equivalents.
    var (_, session) = await Launch(engine, new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL") });
    var type = family == "firefox" ? playwright.Firefox : playwright.Chromium;
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        // chrome.runtime / browser.runtime exist in a page only when an extension talks to it.
        outp["extension_api"] = await page.EvaluateAsync<bool>(api);
        await page.CloseAsync();

        // The Playwright equivalents, in one context.
        var context = await browser.NewContextAsync();
        await context.AddInitScriptAsync(ContentScript); // content script
        await context.SetExtraHTTPHeadersAsync(new Dictionary<string, string> { ["x-my-extension"] = "on" }); // header rewriting
        var blocked = 0; // request blocking, like an ad or image blocker
        await context.RouteAsync(new Regex(@"\.(png|jpe?g|gif|webp)(\?|$)"), route => { blocked++; return route.AbortAsync(); });
        var p = await context.NewPageAsync();
        var injected = 0;
        foreach (var url in new[] { Site, $"{Site}catalogue/page-2.html" })
        {
            await p.GotoAsync(url, new() { Timeout = 60000 });
            if (await p.Locator("#my-extension-banner").CountAsync() > 0) injected++;
        }
        outp["content_script_pages"] = injected;
        outp["blocked_images"] = blocked;
        // httpbin echoes the request headers; read the response body, not the rendered page.
        var resp = await p.GotoAsync("https://httpbin.org/headers", new() { Timeout = 60000 });
        var headers = (await resp!.JsonAsync())!.Value.GetProperty("headers");
        outp["header_added"] = headers.TryGetProperty("X-My-Extension", out var v) && v.GetString() == "on";
    }
    finally
    {
        await browser.CloseAsync();
    }
    return outp;
}

var results = new JsonArray();
foreach (var s in setups) results.Add(await One(s.Label, s.Engine, s.Family, s.Api));
Console.WriteLine(new JsonObject { ["results"] = results }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
