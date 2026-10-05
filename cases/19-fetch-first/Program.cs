// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/154.0.0.0 Safari/537.36";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxyUrl = Environment.GetEnvironmentVariable("PROXY_URL")!;
var proxy = new Uri(proxyUrl);
var creds = proxy.UserInfo.Split(':', 2);
var user = Uri.UnescapeDataString(creds[0]);
var pass = creds.Length > 1 ? Uri.UnescapeDataString(creds[1]) : "";

// Three pages, one question each: is the data in the HTML, or does it need JavaScript?
var targets = new (string Url, string Item, int Expected)[]
{
    ("https://books.toscrape.com/", "product_pod", 20),
    ("https://quotes.toscrape.com/js/", "quote", 10),
    ("https://quotes.toscrape.com/scroll", "quote", 10),
};

static int CountInHtml(string html, string cls) => Regex.Matches(html, $"class=\"[^\"]*\\b{Regex.Escape(cls)}\\b[^\"]*\"").Count;

using var playwright = await Playwright.CreateAsync();

// Step 1: a plain HTTP GET through the same proxy, no browser (Playwright's request API
// runs locally; the proxy keeps the exit IP identical to the browser's).
var http = await playwright.APIRequest.NewContextAsync(new()
{
    Proxy = new Proxy { Server = $"{proxy.Scheme}://{proxy.Host}:{proxy.Port}", Username = user, Password = pass },
    UserAgent = Ua,
});
var rows = new List<JsonObject>();
foreach (var t in targets)
{
    var sw = Stopwatch.StartNew();
    var res = await http.GetAsync(t.Url, new() { Timeout = 60000 });
    var html = await res.TextAsync();
    rows.Add(new JsonObject
    {
        ["url"] = t.Url, ["http_status"] = res.Status, ["html_kb"] = (int)Math.Round(html.Length / 1024.0),
        ["items_in_html"] = CountInHtml(html, t.Item), ["fetch_seconds"] = sw.ElapsedMilliseconds / 1000.0,
    });
}
await http.DisposeAsync();

// Step 2: only the pages whose HTML didn't have the items get a browser.
var needsBrowser = rows.Where((r, i) => (int)r["items_in_html"]! < targets[i].Expected).ToList();
if (needsBrowser.Count > 0)
{
    using var client = new HttpClient();
    client.DefaultRequestHeaders.Add("x-api-key", key);
    var launch = await client.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", new { proxy = proxyUrl, headless = "new" });
    if (!launch.IsSuccessStatusCode) throw new Exception($"launch: {(int)launch.StatusCode} {await launch.Content.ReadAsStringAsync()}");
    var wsUrl = (await launch.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        foreach (var r in needsBrowser)
        {
            var t = targets.First(x => x.Url == (string)r["url"]!);
            var page = await browser.NewPageAsync();
            var sw = Stopwatch.StartNew();
            await page.GotoAsync(t.Url, new() { Timeout = 60000 });
            await page.Locator($".{t.Item}").First.WaitForAsync(new() { Timeout = 60000 });
            r["items_in_browser"] = await page.Locator($".{t.Item}").CountAsync();
            r["browser_seconds"] = sw.ElapsedMilliseconds / 1000.0;
            await page.CloseAsync();
        }
    }
    finally
    {
        await browser.CloseAsync();
    }
}
foreach (var r in rows)
{
    r["needs_browser"] = r.ContainsKey("items_in_browser");
    if (!r.ContainsKey("items_in_browser")) r["items_in_browser"] = null;
    if (!r.ContainsKey("browser_seconds")) r["browser_seconds"] = null;
}
Console.WriteLine(new JsonArray(rows.ToArray()).ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
