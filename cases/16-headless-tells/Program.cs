// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;

// Four ways to run a browser; the same twelve signals read from inside the page.
var targets = new (string Label, string Engine, string Family, JsonObject Body, string Prefix)[]
{
    ("Chromium, headless", "chromium", "chromium", new JsonObject { ["headless"] = "new" }, ""),
    ("Chromium, headful", "chromium", "chromium", new JsonObject { ["headless"] = false }, ""),
    ("Patchright, headful", "patchright", "chromium", new JsonObject { ["headless"] = false }, ""),
    // Camoufox: read the page's own world ("mw:" needs main_world_eval), like a site would.
    ("Camoufox, headful", "camoufox", "firefox", new JsonObject { ["headless"] = false, ["os"] = "windows", ["main_world_eval"] = true }, "mw:"),
};

// The classic headless and automation tells, read the way a detection script reads them.
const string Signals = """
    (async () => {
      let query = null;
      try { query = (await navigator.permissions.query({ name: 'notifications' })).state; } catch { query = 'error'; }
      const gl = (() => {
        try { const g = document.createElement('canvas').getContext('webgl'); const d = g.getExtension('WEBGL_debug_renderer_info'); return g.getParameter(d ? d.UNMASKED_RENDERER_WEBGL : g.RENDERER); } catch { return null; }
      })();
      return {
        ua_says_headless: /Headless/.test(navigator.userAgent),
        webdriver: navigator.webdriver,
        plugins: navigator.plugins.length,
        notification_mismatch: typeof Notification !== 'undefined' && Notification.permission === 'denied' && query === 'prompt',
        outer_window_zero: outerWidth === 0 || outerHeight === 0,
        screen: screen.width + 'x' + screen.height,
        webgl_renderer: gl,
        cores: navigator.hardwareConcurrency,
        memory_gb: navigator.deviceMemory ?? null,
        languages: navigator.languages.join(','),
        chrome_object: typeof window.chrome === 'object' && window.chrome !== null,
      };
    })()
    """;
string[] tellKeys = { "ua_says_headless", "webdriver", "notification_mismatch", "outer_window_zero" };

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> Inspect(string label, string engine, string family, JsonObject body, string prefix)
{
    var options = body.DeepClone().AsObject();
    options["proxy"] = proxy;
    var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
        new StringContent(options.ToJsonString(), System.Text.Encoding.UTF8, "application/json"));
    if (!res.IsSuccessStatusCode) return new JsonObject { ["label"] = label, ["error"] = $"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var session = await res.Content.ReadFromJsonAsync<JsonElement>();
    var type = family == "firefox" ? playwright.Firefox : playwright.Chromium;
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        await page.GotoAsync("https://example.com/", new() { Timeout = 60000 });
        var signals = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(prefix + Signals)).GetRawText())!.AsObject();
        var outp = new JsonObject { ["label"] = label, ["threads"] = session.GetProperty("weight").GetInt32() };
        foreach (var (k, v) in signals) outp[k] = v?.DeepClone();
        var tells = tellKeys.Where(k => signals[k] is JsonValue v && v.TryGetValue<bool>(out var b) && b).ToList();
        outp["tells"] = tells.Count > 0 ? string.Join(", ", tells) : "none";
        return outp;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var rows = await Task.WhenAll(targets.Select(t => Inspect(t.Label, t.Engine, t.Family, t.Body, t.Prefix)));
Console.WriteLine(new JsonArray(rows.ToArray<JsonNode?>()).ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
