// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, SOCKS_PROXIES (comma-separated socks5:// URLs)
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var nextProxy = Environment.GetEnvironmentVariable("SOCKS_PROXIES")!.Split(',')[0];
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), proxy_updatable = true, headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var session = await res.Content.ReadFromJsonAsync<JsonElement>();
var sessionId = session.GetProperty("sessionId").GetString()!;
var wsUrl = session.GetProperty("wsUrl").GetString()!;

// Swap the proxy on the session's router (the host in wsUrl). Takes effect for new
// connections; the browser, its tabs, cookies and storage stay as they are.
async Task SwapProxy(string proxy)
{
    var r = await http.PostAsJsonAsync($"https://{new Uri(wsUrl).Host}/admin/session/proxy", new { session_id = sessionId, proxy });
    if (!r.IsSuccessStatusCode) throw new Exception($"swap: {(int)r.StatusCode} {await r.Content.ReadAsStringAsync()}");
}

async Task<JsonNode> Json(IPage page, string url) =>
    JsonNode.Parse(await (await page.GotoAsync(url, new() { Timeout = 60000 }))!.TextAsync())!;
async Task<string> ExitIp(IPage page, string host) => (string)(await Json(page, $"https://{host}/?format=json"))["ip"]!;
async Task<JsonNode> Cookies(IPage page) => (await Json(page, "https://httpbin.org/cookies"))["cookies"]!.DeepClone();

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    await page.GotoAsync("https://httpbin.org/cookies/set?cart=3-items&login=alice", new() { Timeout = 60000 });
    await page.EvaluateAsync("localStorage.setItem('draft', 'half-written review')");
    var before = new JsonObject { ["exit_ip"] = await ExitIp(page, "api.ipify.org"), ["cookies"] = await Cookies(page) };

    var sw = Stopwatch.StartNew();
    await SwapProxy(nextProxy);
    var swapMs = sw.ElapsedMilliseconds;

    // 1. Same tab, same host: the open keep-alive connection still goes through the old proxy.
    var reusedConnection = await ExitIp(page, "api.ipify.org");
    // 2. Same tab, a host we haven't connected to yet: a new connection, so the new proxy.
    var newConnection = await ExitIp(page, "api64.ipify.org");
    // 3. Move everything over: a new context (its own connection pool) with the old
    //    cookies and localStorage.
    var moved = await browser.NewContextAsync(new() { StorageState = await context.StorageStateAsync() });
    await context.CloseAsync();
    var page2 = await moved.NewPageAsync();
    var after = new JsonObject
    {
        ["exit_ip"] = await ExitIp(page2, "api.ipify.org"),
        ["cookies"] = await Cookies(page2),
        ["local_storage"] = await page2.EvaluateAsync<string>("localStorage.getItem('draft')"),
    };
    var result = new JsonObject
    {
        ["before"] = before,
        ["swap_ms"] = swapMs,
        ["same_tab_reused_connection"] = reusedConnection,
        ["same_tab_new_connection"] = newConnection,
        ["new_context_with_storage_state"] = after,
        ["same_browser_session"] = browser.IsConnected,
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
