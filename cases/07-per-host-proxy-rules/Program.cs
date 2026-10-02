// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs, 3 or more)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var socks = Environment.GetEnvironmentVariable("SOCKS_PROXIES")!.Split(',');
string HostOf(string url) => new Uri(url).Host;

var options = new
{
    proxy = Environment.GetEnvironmentVariable("PROXY_URL"), // everything not matched below
    proxy_rules = new object[]
    {
        new { hosts = new[] { "*.ident.me", "ident.me" }, proxy = socks[0] },
        new { hosts = new[] { "httpbin.org", "*.httpbin.org" }, proxy = new[] { socks[1], socks[2] } }, // round-robin
        // IP-lookup services always use the default proxy, so this rule is ignored on purpose.
        new { hosts = new[] { "api.ipify.org" }, proxy = socks[0] },
    },
    headless = true,
};
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", options);
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// Each context has its own connection pool, so each reading is a fresh connection.
// Proxies drop a connection now and then: one retry.
async Task<string> ExitIpVia(string url)
{
    for (var attempt = 1; ; attempt++)
    {
        var context = await browser.NewContextAsync();
        try
        {
            var page = await context.NewPageAsync();
            var text = await (await page.GotoAsync(url, new() { Timeout = 30000 }))!.TextAsync();
            var m = Regex.Match(text, @"\d{1,3}(\.\d{1,3}){3}");
            return m.Success ? m.Value : $"(no IP in {url})";
        }
        // .NET reports navigation timeouts as TimeoutException, not PlaywrightException.
        catch (Exception err) when ((err is PlaywrightException or TimeoutException) && attempt < 2) { }
        catch (Exception err) when (err is PlaywrightException or TimeoutException) { return $"(failed: {err.Message.Split('\n')[0]})"; }
        finally { await context.CloseAsync(); }
    }
}

try
{
    var readings = new JsonArray();
    foreach (var url in new[] { "https://v4.ident.me/", "https://httpbin.org/ip", "https://httpbin.org/ip", "https://httpbin.org/ip",
                                "https://api.ipify.org/", "https://www.cloudflare.com/cdn-cgi/trace" })
        readings.Add(new JsonObject { ["url"] = url, ["exit_ip"] = await ExitIpVia(url) });
    var result = new JsonObject
    {
        ["rules"] = new JsonObject
        {
            ["*.ident.me"] = HostOf(socks[0]),
            ["httpbin.org"] = new JsonArray(HostOf(socks[1]), HostOf(socks[2])),
            ["api.ipify.org"] = $"{HostOf(socks[0])} (ignored: IP-lookup host)",
            ["(everything else)"] = "residential PROXY_URL",
        },
        ["readings"] = readings,
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
