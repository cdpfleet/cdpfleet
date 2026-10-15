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
const string Site = "https://shop.example.test"; // doesn't exist: every request to it is answered by the script

// A tiny shop front: loads products from its API, shows an error state, and a slideshow title
// from a real public API (httpbin.org/json).
var html = "<html><head><meta charset=\"utf-8\"></head><body><h1>Shop</h1><ul id=\"list\"></ul><p id=\"status\"></p><p id=\"title\"></p>"
    + $"<img src=\"{Site}/banner.png\"><img src=\"https://httpbin.org/image/png\"><script>"
    + "fetch('/api/products').then((r) => r.ok ? r.json() : Promise.reject(r.status))"
    + ".then((items) => { for (const p of items) document.getElementById('list').insertAdjacentHTML('beforeend', '<li>' + p.name + ' — ' + p.price + '</li>'); document.getElementById('status').textContent = 'ok'; })"
    + ".catch((code) => { document.getElementById('status').textContent = 'error ' + code; });"
    + "fetch('https://httpbin.org/json').then((r) => r.json()).then((j) => { document.getElementById('title').textContent = j.slideshow.title; });"
    + "</script></body></html>";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {engine} {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// Open the shop with the API answering `api` ("stub" or "error"); returns what the page shows.
async Task<JsonObject> Visit(IBrowserContext context, string api)
{
    var page = await context.NewPageAsync();
    var aborted = 0;
    // The page itself and its API never leave the browser (the page also declares its charset).
    await page.RouteAsync($"{Site}/", route => route.FulfillAsync(new() { ContentType = "text/html; charset=utf-8", Body = html }));
    await page.RouteAsync($"{Site}/api/products", route => api == "stub"
        ? route.FulfillAsync(new() { Json = new[] { new { name = "Red mug", price = "$9" }, new { name = "Blue mug", price = "$11" }, new { name = "Teapot", price = "$24" } } })
        : route.FulfillAsync(new() { Status = 500, Body = "boom" }));
    // A real response, changed on its way in.
    await page.RouteAsync("https://httpbin.org/json", async route =>
    {
        var response = await route.FetchAsync();
        var json = JsonNode.Parse(await response.TextAsync())!;
        json["slideshow"]!["title"] = "Patched: " + json["slideshow"]!["title"]!.GetValue<string>();
        await route.FulfillAsync(new() { Response = response, Body = json.ToJsonString() });
    });
    // Images are dropped before they're requested.
    await page.RouteAsync(new Regex(@"\.png$|/image/"), route => { aborted++; return route.AbortAsync(); });
    await page.GotoAsync($"{Site}/");
    await page.WaitForFunctionAsync("() => document.getElementById('status').textContent && document.getElementById('title').textContent", null, new() { Timeout = 30000 });
    var shown = await page.EvaluateAsync<JsonElement>("() => ({ items: [...document.querySelectorAll('#list li')].map((li) => li.textContent), status: document.getElementById('status').textContent, title: document.getElementById('title').textContent })");
    await page.CloseAsync();
    var outp = JsonNode.Parse(shown.GetRawText())!.AsObject();
    outp["images_aborted"] = aborted;
    return outp;
}

// Does the charset in the fulfilled Content-Type alone decode non-ASCII text? (No <meta>.)
async Task<string> CharsetHeaderOnly(IBrowserContext context)
{
    var page = await context.NewPageAsync();
    await page.RouteAsync($"{Site}/plain", route => route.FulfillAsync(new() { ContentType = "text/html; charset=utf-8", Body = "<p>Red mug — $9</p>" }));
    await page.GotoAsync($"{Site}/plain");
    var text = await page.TextContentAsync("p") ?? "";
    await page.CloseAsync();
    return text;
}

async Task<JsonObject> One(string engine)
{
    var session = await Launch(engine, new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = true, ["record"] = false });
    var type = engine switch { "firefox" => playwright.Firefox, "webkit" => playwright.Webkit, _ => playwright.Chromium };
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var context = await browser.NewContextAsync();
        var stub = await Visit(context, "stub");
        var error = await Visit(context, "error");
        var headerOnly = await CharsetHeaderOnly(context);
        return new JsonObject
        {
            ["engine"] = engine, ["items"] = stub["items"]!.DeepClone(), ["status"] = stub["status"]!.DeepClone(), ["title"] = stub["title"]!.DeepClone(),
            ["images_aborted"] = stub["images_aborted"]!.DeepClone(), ["error_status"] = error["status"]!.DeepClone(), ["header_only_text"] = headerOnly,
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var results = new JsonArray();
foreach (var engine in new[] { "chromium", "firefox", "webkit" }) results.Add(await One(engine));
Console.WriteLine(new JsonObject { ["results"] = results }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
