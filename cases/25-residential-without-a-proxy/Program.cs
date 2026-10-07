// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (no proxy of your own needed)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

static string Base36(long n)
{
    const string digits = "0123456789abcdefghijklmnopqrstuvwxyz";
    var s = "";
    while (n > 0) { s = digits[(int)(n % 36)] + s; n /= 36; }
    return s;
}
var b36 = Base36(DateTimeOffset.UtcNow.ToUnixTimeMilliseconds());
var session = "c25" + b36[^6..]; // a sticky name, 1–10 of [A-Za-z0-9_]

// Four launches, all on the cdpfleet residential proxy — the token is the whole proxy config.
var runs = new[]
{
    (Label: "rotating, any country", Proxy: "cdpfleet-resi"),
    (Label: "rotating, Germany", Proxy: "cdpfleet-resi-country-de"),
    (Label: "sticky US, browser 1", Proxy: $"cdpfleet-resi-country-us-session-{session}-lifetime-10"),
    (Label: "sticky US, browser 2", Proxy: $"cdpfleet-resi-country-us-session-{session}-lifetime-10"),
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// Residential peers drop a few percent of connections: retry a lookup up to 3 times.
async Task<JsonElement> Lookup(IPage page, int i)
{
    for (var attempt = 1; ; attempt++)
    {
        try
        {
            var r = await page.GotoAsync($"http://ip-api.com/json/?fields=query,countryCode&i={i}-{attempt}", new() { Timeout = 60000 });
            return JsonDocument.Parse(await r!.TextAsync()).RootElement.Clone();
        }
        catch (Exception e) when (e is PlaywrightException or TimeoutException or JsonException)
        {
            if (attempt == 3) throw;
        }
    }
}

async Task<(JsonObject Row, string? FirstIp)> Run(string label, string proxy)
{
    var row = new JsonObject { ["label"] = label };
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", new { proxy, headless = "new" });
    if (!res.IsSuccessStatusCode)
    {
        row["error"] = $"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}";
        return (row, null);
    }
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        // Three lookups on separate connections: rotating exits change, sticky ones don't.
        var exits = new List<JsonElement>();
        for (var i = 0; i < 3; i++) exits.Add(await Lookup(page, i));
        row["proxy"] = proxy.Replace(session, "<name>");
        row["countries"] = string.Join(",", exits.Select(e => e.GetProperty("countryCode").GetString()).Distinct());
        row["distinct_ips"] = exits.Select(e => e.GetProperty("query").GetString()).Distinct().Count();
        return (row, exits[0].GetProperty("query").GetString());
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var results = new List<(JsonObject Row, string? FirstIp)>();
foreach (var r in runs) results.Add(await Run(r.Label, r.Proxy)); // in order, so browser 2 starts after browser 1 ended
var b1Ip = results[2].FirstIp;
foreach (var (row, ip) in results)
    row["same_ip_as_browser_1"] = row["label"]!.GetValue<string>().StartsWith("sticky") ? JsonValue.Create(ip == b1Ip) : null;
// The balance the traffic is billed to (metered about once a minute, so it trails a little).
var bal = await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me/proxy-balance");
var doc = new JsonObject
{
    ["runs"] = new JsonArray(results.Select(x => (JsonNode)x.Row).ToArray()),
    ["sticky_ip_kept_across_browsers"] = results[3].Row["same_ip_as_browser_1"]?.DeepClone(),
    ["balance_allowed"] = bal.GetProperty("allowed").GetBoolean(),
    ["price_per_gb_usd"] = JsonNode.Parse(bal.GetProperty("price_per_gb_usd").GetRawText()),
};
Console.WriteLine(doc.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
