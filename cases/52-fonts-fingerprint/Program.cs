// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// The families a font-fingerprinting script typically tries: Windows, macOS, Linux and popular extras.
string[] candidates =
[
    "Arial", "Arial Black", "Calibri", "Cambria", "Candara", "Comic Sans MS", "Consolas", "Constantia", "Corbel", "Courier New",
    "Georgia", "Impact", "Lucida Console", "Lucida Sans Unicode", "Microsoft Sans Serif", "Palatino Linotype", "Segoe UI", "Tahoma",
    "Times New Roman", "Trebuchet MS", "Verdana", "American Typewriter", "Avenir", "Avenir Next", "Futura", "Geneva", "Gill Sans",
    "Helvetica", "Helvetica Neue", "Lucida Grande", "Menlo", "Monaco", "Optima", "SF Pro", "DejaVu Sans", "DejaVu Serif",
    "Liberation Sans", "Liberation Mono", "Noto Sans", "Ubuntu", "Cantarell", "Roboto", "Open Sans", "Fira Code", "JetBrains Mono",
    "Source Code Pro",
];

// The classic width probe: a family is "installed" if text set in it, with a generic fallback,
// measures differently from the generic family alone.
const string Probe = @"(fonts) => {
  const span = document.createElement('span');
  span.textContent = 'mmmmmmmmmmlli10OWQ@#';
  span.style.cssText = 'position:absolute;left:-9999px;font-size:72px';
  document.body.appendChild(span);
  const size = (family) => { span.style.fontFamily = family; return span.offsetWidth + 'x' + span.offsetHeight; };
  const generics = ['monospace', 'serif', 'sans-serif'];
  const base = {};
  for (const g of generics) base[g] = size(g);
  const found = fonts.filter((f) => generics.some((g) => size('""' + f + '"", ' + g) !== base[g]));
  span.remove();
  return { platform: navigator.platform, found, generic_sizes: generics.map((g) => base[g]) };
}";

var setups = new (string Name, JsonObject Body)[]
{
    ("os: windows (launch 1)", new JsonObject { ["os"] = "windows" }),
    ("os: windows (launch 2)", new JsonObject { ["os"] = "windows" }),
    ("os: macos", new JsonObject { ["os"] = "macos" }),
    ("os: linux", new JsonObject { ["os"] = "linux" }),
    ("windows + fonts", new JsonObject { ["os"] = "windows", ["fonts"] = new JsonArray("Menlo", "Helvetica Neue", "Fira Code") }),
    ("windows + fonts + custom_fonts_only", new JsonObject { ["os"] = "windows", ["fonts"] = new JsonArray("Calibri", "Consolas", "Segoe UI", "Fira Code"), ["custom_fonts_only"] = true }),
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync("https://starter.cdpfleet.com/camoufox/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// Proxies drop a tunnel now and then: up to two retries.
async Task Go(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(url, new() { Timeout = 60000 }); return; }
        catch (PlaywrightException) when (attempt < 3) { await Task.Delay(2000); }
    }
}

var results = new JsonArray();
foreach (var (name, setupBody) in setups)
{
    var body = (JsonObject)setupBody.DeepClone();
    body["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL");
    body["headless"] = true;
    body["record"] = false;
    var session = await Launch(body);
    var browser = await playwright.Firefox.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await (await browser.NewContextAsync()).NewPageAsync();
        await Go(page, "https://httpbin.org/html");
        var r = await page.EvaluateAsync<JsonElement>(Probe, candidates);
        var found = r.GetProperty("found").EnumerateArray().Select((f) => f.GetString()!).ToList();
        results.Add(new JsonObject
        {
            ["setup"] = name,
            ["platform"] = r.GetProperty("platform").GetString(),
            ["detected"] = found.Count,
            ["font_hash"] = Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(string.Join(",", found)))).ToLower()[..10],
            ["generics_identical"] = r.GetProperty("generic_sizes").EnumerateArray().Select((g) => g.GetString()).Distinct().Count() == 1,
            ["fonts"] = new JsonArray(found.Select((f) => (JsonNode)JsonValue.Create(f)!).ToArray()),
        });
    }
    finally
    {
        await browser.CloseAsync();
    }
}
Console.WriteLine(results.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
