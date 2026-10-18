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
      "proxy": "http://user:pass@proxy.example.com:8080",
      "headless": "new",
      "keep_alive": true,
      "cdp": true,
      "inactivity_timeout": "10m"
    }
    """;
var res = await http.PostAsync("https://starter.cdpfleet.com/edge/session",
    new StringContent(body, Encoding.UTF8, "application/json"));
if (!res.IsSuccessStatusCode)
    throw new Exception($"launch failed: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var cdpUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("cdpUrl").GetString()!;

// 2. Connect and drive it
using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectOverCDPAsync(cdpUrl, new BrowserTypeConnectOverCDPOptions
{
    Headers = new Dictionary<string, string> { ["x-api-key"] = key },
});
var context = browser.Contexts[0]; // the default context — it survives a reconnect
var page = context.Pages.Count > 0 ? context.Pages[0] : await context.NewPageAsync();
await page.GotoAsync("https://example.com");
Console.WriteLine(await page.TitleAsync());
await browser.CloseAsync(); // with keep_alive this only disconnects: end the session with DELETE /v1/me/threads/{sessionId}, or it ends after inactivity_timeout
