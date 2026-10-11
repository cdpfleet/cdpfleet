// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

// What a page can learn about where the visitor is, from JavaScript.
const string Probe = """
    async () => {
      const geo = await new Promise((resolve) => navigator.geolocation.getCurrentPosition(
        (p) => resolve({ lat: Math.round(p.coords.latitude * 100) / 100, lon: Math.round(p.coords.longitude * 100) / 100 }),
        (e) => resolve({ error: e.code === 1 ? 'permission denied' : e.message }),
        { timeout: 5000 }));
      const perm = (await navigator.permissions.query({ name: 'geolocation' })).state;
      return { geolocation: geo, permission: perm, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, language: navigator.language, utc_offset_min: -new Date().getTimezoneOffset() };
    }
    """;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL_DE"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    JsonObject? ip = null;
    var contexts = new JsonArray();
    var setups = new (string Label, BrowserNewContextOptions Options)[]
    {
        ("defaults", new()),
        ("permission granted, no position", new() { Permissions = ["geolocation"] }),
        ("Berlin: geolocation + timezone + locale", new()
        {
            Permissions = ["geolocation"],
            Geolocation = new() { Latitude = 52.52f, Longitude = 13.405f },
            TimezoneId = "Europe/Berlin",
            Locale = "de-DE",
        }),
    };
    foreach (var (label, options) in setups)
    {
        var ctx = await browser.NewContextAsync(options);
        var page = await ctx.NewPageAsync();
        if (ip is null)
        {
            await page.GotoAsync("http://ip-api.com/json/?fields=query,countryCode,timezone", new() { Timeout = 60000 });
            var info = JsonNode.Parse((await page.TextContentAsync("body"))!)!;
            ip = new JsonObject { ["country"] = info["countryCode"]?.DeepClone(), ["timezone"] = info["timezone"]?.DeepClone() };
        }
        await page.GotoAsync("https://httpbin.org/html", new() { Timeout = 60000 });
        var entry = new JsonObject { ["setup"] = label };
        foreach (var prop in ((JsonObject)Plain(await page.EvaluateAsync<JsonElement>($"({Probe})()"))!).ToList())
            entry[prop.Key] = prop.Value?.DeepClone();
        contexts.Add(entry);
        await ctx.CloseAsync();
    }

    var result = new JsonObject { ["ip"] = ip, ["contexts"] = contexts };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}

// EvaluateAsync<JsonElement> tags every object with a "$id" reference key; drop those.
static JsonNode? Plain(JsonElement e) => e.ValueKind switch
{
    JsonValueKind.Object => new JsonObject(e.EnumerateObject().Where(p => p.Name != "$id")
        .Select(p => KeyValuePair.Create(p.Name, Plain(p.Value)))),
    JsonValueKind.Array => new JsonArray(e.EnumerateArray().Select(Plain).ToArray()),
    _ => JsonNode.Parse(e.GetRawText()),
};
