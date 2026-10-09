// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string PageUrl = "https://books.toscrape.com/";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

var dir = Path.Combine(Path.GetTempPath(), $"cdpfleet-captures-{DateTimeOffset.UtcNow.ToUnixTimeMilliseconds()}");
Directory.CreateDirectory(dir);

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync(new() { ViewportSize = new() { Width = 1280, Height = 720 } });
    await page.GotoAsync(PageUrl, new() { Timeout = 60000, WaitUntil = WaitUntilState.NetworkIdle });

    var results = new JsonArray();

    async Task<JsonObject> Capture(string label, Func<string, Task> fn)
    {
        var ext = label.Contains("pdf") ? ".pdf" : ".png";
        var path = Path.Combine(dir, label.Replace(' ', '-') + ext);
        var sw = Stopwatch.StartNew();
        await fn(path);
        var size = new FileInfo(path).Length;
        File.Delete(path);
        return new JsonObject
        {
            ["type"] = label,
            ["file_size_bytes"] = size,
            ["seconds"] = Math.Round(sw.Elapsed.TotalSeconds, 2),
        };
    }

    results.Add(await Capture("viewport screenshot",
        p => page.ScreenshotAsync(new() { Path = p })));
    results.Add(await Capture("full-page screenshot",
        p => page.ScreenshotAsync(new() { Path = p, FullPage = true })));
    results.Add(await Capture("element screenshot",
        p => page.Locator(".product_pod").First.ScreenshotAsync(new() { Path = p })));
    results.Add(await Capture("pdf",
        p => page.PdfAsync(new() { Path = p })));

    var output = new JsonObject { ["captures"] = results };
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
    if (Directory.Exists(dir)) Directory.Delete(dir, true);
}
