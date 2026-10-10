// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

// A static catalogue, a big article and a page that renders its data with a delayed script.
var pages = new (string Url, string Data)[]
{
    ("https://books.toscrape.com/", "article.product_pod"),
    ("https://en.wikipedia.org/wiki/Web_scraping", "#mw-content-text p"),
    ("https://quotes.toscrape.com/js-delayed/", ".quote"),
};
var strategies = new[] { "commit", "domcontentloaded", "load", "networkidle", "selector" };
var waitUntil = new Dictionary<string, WaitUntilState>
{
    ["commit"] = WaitUntilState.Commit,
    ["domcontentloaded"] = WaitUntilState.DOMContentLoaded,
    ["load"] = WaitUntilState.Load,
    ["networkidle"] = WaitUntilState.NetworkIdle,
};

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
    var rows = new JsonArray();
    foreach (var (url, data) in pages)
    {
        foreach (var strategy in strategies)
        {
            // A fresh context each time: no cache, so every strategy waits for the same work.
            var context = await browser.NewContextAsync();
            var page = await context.NewPageAsync();
            var sw = System.Diagnostics.Stopwatch.StartNew();
            if (strategy == "selector")
            {
                await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.Commit, Timeout = 60000 });
                await page.Locator(data).First.WaitForAsync(new() { Timeout = 60000 });
            }
            else
            {
                await page.GotoAsync(url, new() { WaitUntil = waitUntil[strategy], Timeout = 60000 });
            }
            var ms = sw.ElapsedMilliseconds;
            rows.Add(new JsonObject
            {
                ["url"] = url,
                ["strategy"] = strategy,
                ["ms"] = ms,
                ["items_ready"] = await page.Locator(data).CountAsync(),
            });
            await context.CloseAsync();
        }
    }

    var result = new JsonObject { ["rows"] = rows };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
