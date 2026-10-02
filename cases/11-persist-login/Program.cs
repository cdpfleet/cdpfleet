// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var stateFile = Path.Combine(Path.GetTempPath(), "cdpfleet-state.json"); // keep it safe: it holds the login
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();
var headers = new Dictionary<string, string> { ["x-api-key"] = key };

async Task<JsonElement> Launch(string name)
{
    var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{name}/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {name}: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    return await res.Content.ReadFromJsonAsync<JsonElement>();
}

async Task<JsonNode> CookiesSeen(IPage page) =>
    JsonNode.Parse(await (await page.GotoAsync("https://httpbin.org/cookies", new() { Timeout = 60000 }))!.TextAsync())!["cookies"]!.DeepClone();

// Session 1 (Chromium): "log in", then save cookies + localStorage to a local file.
var s1 = await Launch("chromium");
var b1 = await playwright.Chromium.ConnectAsync(s1.GetProperty("wsUrl").GetString()!, new() { Headers = headers });
JsonNode saved;
try
{
    var ctx = await b1.NewContextAsync();
    var page = await ctx.NewPageAsync();
    await page.GotoAsync("https://httpbin.org/cookies/set?session=abc123&user=alice", new() { Timeout = 60000 });
    await page.EvaluateAsync("localStorage.setItem('draft', 'half-written review')");
    saved = JsonNode.Parse(await ctx.StorageStateAsync(new() { Path = stateFile }))!;
}
finally
{
    await b1.CloseAsync(); // the browser is gone; only the state file remains
}

// Session 2 (Firefox, a fresh browser on whichever server the fleet picks): restore it.
var s2 = await Launch("firefox");
var b2 = await playwright.Firefox.ConnectAsync(s2.GetProperty("wsUrl").GetString()!, new() { Headers = headers });
try
{
    var page = await (await b2.NewContextAsync(new() { StorageStatePath = stateFile })).NewPageAsync();
    var cookies = await CookiesSeen(page);
    var draft = await page.EvaluateAsync<string>("localStorage.getItem('draft')");
    var blankCookies = await CookiesSeen(await (await b2.NewContextAsync()).NewPageAsync()); // the same browser without the state
    var result = new JsonObject
    {
        ["session_1"] = new JsonObject
        {
            ["id"] = s1.GetProperty("sessionId").GetString(),
            ["browser"] = "chromium",
            ["saved_cookies"] = new JsonArray(saved["cookies"]!.AsArray().Select(c => (JsonNode?)$"{c!["name"]}@{c["domain"]}").ToArray()),
            ["saved_origins"] = new JsonArray(saved["origins"]!.AsArray().Select(o => (JsonNode?)(string)o!["origin"]!).ToArray()),
        },
        ["state_file_bytes"] = new FileInfo(stateFile).Length,
        ["session_2"] = new JsonObject
        {
            ["id"] = s2.GetProperty("sessionId").GetString(), ["browser"] = "firefox", ["cookies_sent"] = cookies, ["local_storage_draft"] = draft,
        },
        ["session_2_without_state"] = new JsonObject { ["cookies_sent"] = blankCookies },
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await b2.CloseAsync();
}
