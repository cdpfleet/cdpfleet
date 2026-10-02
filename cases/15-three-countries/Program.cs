// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US, PROXY_URL_DE, PROXY_URL_JP (exits in each country)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var countries = new (string Country, string Locale, string Proxy)[]
{
    ("United States", "en-US", Environment.GetEnvironmentVariable("PROXY_URL_US")!),
    ("Germany", "de-DE", Environment.GetEnvironmentVariable("PROXY_URL_DE")!),
    ("Japan", "ja-JP", Environment.GetEnvironmentVariable("PROXY_URL_JP")!),
};

// What a localizing site reads in the page. Run in the PAGE's own JavaScript world
// ("mw:" prefix, needs main_world_eval): Playwright's default isolated world isn't patched
// the same way and can report the server's UTC timezone instead of the persona's.
const string LocalView = """
    (async () => {
      const position = await new Promise((ok) => navigator.geolocation.getCurrentPosition(
        (p) => ok({ lat: p.coords.latitude, lon: p.coords.longitude }), () => ok(null), { timeout: 10000 }));
      return {
        languages: navigator.languages,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        date: new Date('2026-10-01T15:30:00Z').toLocaleString(),
        number: (1234567.891).toLocaleString(),
        price: new Intl.NumberFormat(undefined, { style: 'currency', currency: 'EUR' }).format(49.9),
        position,
      };
    })()
    """;

// Great-circle distance in km.
long Km(double lat1, double lon1, double lat2, double lon2)
{
    double R(double d) => d * Math.PI / 180;
    var h = Math.Pow(Math.Sin(R(lat2 - lat1) / 2), 2) + Math.Cos(R(lat1)) * Math.Cos(R(lat2)) * Math.Pow(Math.Sin(R(lon2 - lon1) / 2), 2);
    return (long)Math.Round(12742 * Math.Asin(Math.Sqrt(h)));
}

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> Persona(string country, string locale, string proxy)
{
    // geoip: Camoufox sets timezone and geolocation from the proxy's exit IP at launch.
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/camoufox/session",
        new { proxy, headless = true, os = "windows", locale, geoip = true, main_world_eval = true });
    if (!res.IsSuccessStatusCode) return new JsonObject { ["country"] = country, ["error"] = $"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Firefox.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var context = await browser.NewContextAsync();
        await context.GrantPermissionsAsync(new[] { "geolocation" }); // as if the visitor clicked "Allow"
        var page = await context.NewPageAsync();
        var exit = JsonNode.Parse(await (await page.GotoAsync("http://ip-api.com/json/?fields=country,city,timezone,lat,lon", new() { Timeout = 60000 }))!.TextAsync())!;
        await page.GotoAsync("https://httpbin.org/html", new() { Timeout = 60000 });
        var seen = JsonNode.Parse((await page.EvaluateAsync<JsonElement>("mw:" + LocalView)).GetRawText())!.AsObject();
        var isolated = await page.EvaluateAsync<string>("Intl.DateTimeFormat().resolvedOptions().timeZone");
        var outp = new JsonObject
        {
            ["country"] = country, ["locale"] = locale, ["exit"] = $"{exit["city"]}, {exit["country"]}", ["exit_timezone"] = exit["timezone"]!.DeepClone(),
        };
        foreach (var (k, v) in seen) outp[k] = v?.DeepClone();
        var pos = seen["position"];
        outp["position_km_from_exit"] = pos is null ? null
            : Km((double)pos["lat"]!, (double)pos["lon"]!, (double)exit["lat"]!, (double)exit["lon"]!);
        outp["isolated_world_timezone"] = isolated; // what a default page.evaluate would have reported
        return outp;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var rows = await Task.WhenAll(countries.Select(c => Persona(c.Country, c.Locale, c.Proxy)));
Console.WriteLine(new JsonArray(rows.ToArray<JsonNode?>()).ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
