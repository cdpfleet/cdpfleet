// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Start = "https://books.toscrape.com/";
const string ExtractJs = """
    () => [...document.querySelectorAll('article.product_pod')].map(el => {
      const stars = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };
      const ratingClass = [...el.querySelector('.star-rating').classList].find(c => c !== 'star-rating');
      return {
        title: el.querySelector('h3 a').getAttribute('title'),
        price: parseFloat(el.querySelector('.price_color').textContent.replace(/[^0-9.]/g, '')),
        rating: stars[ratingClass] || 0,
        in_stock: el.querySelector('.availability').textContent.trim().toLowerCase().includes('in stock'),
      };
    })
    """;

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
    var sw = System.Diagnostics.Stopwatch.StartNew();
    var books = new JsonArray();

    await page.GotoAsync(Start, new() { Timeout = 60000 });
    foreach (var el in (await page.EvaluateAsync<JsonElement>(ExtractJs)).EnumerateArray())
        books.Add(JsonNode.Parse(el.GetRawText()));

    var next = page.Locator("li.next a");
    if (await next.CountAsync() > 0)
    {
        await next.ClickAsync();
        await page.WaitForLoadStateAsync(LoadState.DOMContentLoaded);
        foreach (var el in (await page.EvaluateAsync<JsonElement>(ExtractJs)).EnumerateArray())
            books.Add(JsonNode.Parse(el.GetRawText()));
    }

    var result = new JsonObject
    {
        ["pages_scraped"] = 2,
        ["total_books"] = books.Count,
        ["books"] = books,
        ["seconds"] = Math.Round(sw.Elapsed.TotalSeconds, 2),
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
