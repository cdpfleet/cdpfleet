// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

string[] urls = ["https://news.ycombinator.com/", "https://en.wikipedia.org/wiki/Web_scraping", "https://books.toscrape.com/"];

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
    var page = await browser.NewPageAsync();
    var pages = new JsonArray();
    foreach (var url in urls)
    {
        await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 60000 });
        var html = await page.ContentAsync();
        var text = await page.Locator("body").InnerTextAsync();
        var aria = await page.Locator("body").AriaSnapshotAsync();
        pages.Add(new JsonObject
        {
            ["url"] = url,
            ["html_chars"] = html.Length,
            ["text_chars"] = text.Length,
            ["aria_chars"] = aria.Length,
            ["aria_links"] = aria.Split("- link ").Length - 1,
            ["aria_vs_html"] = Math.Round((double)aria.Length / html.Length * 1000, MidpointRounding.AwayFromZero) / 10,
            ["aria_sample"] = string.Join("\n", aria.Split('\n').Take(6)),
        });
    }
    var result = new JsonObject { ["pages"] = pages };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
