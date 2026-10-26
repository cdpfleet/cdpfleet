// dotnet add package Microsoft.Playwright --version 1.60.0
// Browsers run on cdpfleet, so no `playwright install` is needed.
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// 1. Launch the browser
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var body = """
    {
      "proxy": "cdpfleet-resi-country-us-session-cart1-lifetime-30",
      "headless": "new"
    }
    """;
var res = await http.PostAsync("https://starter.cdpfleet.com/chromium/session",
    new StringContent(body, Encoding.UTF8, "application/json"));
if (!res.IsSuccessStatusCode)
    throw new Exception($"launch failed: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

// 2. Connect and drive it
using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new BrowserTypeConnectOptions
{
    Headers = new Dictionary<string, string> { ["x-api-key"] = key },
});
var page = await browser.NewPageAsync();
await page.GotoAsync("https://example.com");
Console.WriteLine(await page.TitleAsync());
await browser.CloseAsync(); // ends the session and stops billing
