// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// The checks a bot-detection script typically runs, as a page script (not page.evaluate),
// exactly as a website would run them.
const string Checks = """
    <script>
    window.__checks = (async () => {
      const gl = document.createElement('canvas').getContext('webgl');
      const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
      const perm = await navigator.permissions.query({ name: 'notifications' });
      return {
        webdriver: navigator.webdriver,
        headless_in_ua: /Headless/.test(navigator.userAgent),
        window_chrome: typeof window.chrome === 'object',
        plugins: navigator.plugins.length,
        languages: navigator.languages.join(','),
        webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
        notification_permission_mismatch: Notification.permission === 'denied' && perm.state === 'prompt',
        outer_minus_inner_height: outerHeight - innerHeight,
      };
    })();
    </script>
    """;

async Task<JsonObject> Check(string name, bool headless)
{
    var mode = headless ? "headless" : "headful";
    var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{name}/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless });
    if (!res.IsSuccessStatusCode) return new JsonObject { ["build"] = name, ["mode"] = mode, ["error"] = $"{(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        // A real https origin: some APIs (permissions, WebGL info) behave differently on about:blank.
        await page.RouteAsync("https://detect.example/", route => route.FulfillAsync(new() { ContentType = "text/html", Body = Checks }));
        await page.GotoAsync("https://detect.example/");
        var checks = JsonNode.Parse((await page.EvaluateAsync<JsonElement>("window.__checks")).GetRawText())!.AsObject();
        var outp = new JsonObject { ["build"] = name, ["mode"] = mode, ["version"] = browser.Version };
        foreach (var (k, v) in checks) outp[k] = v?.DeepClone();
        return outp;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

// Every build twice: headless (1 thread) and headful on a virtual display (2 threads).
var builds = new[] { "chromium", "chrome", "patchright", "cloakbrowser" };
var rows = await Task.WhenAll(builds.SelectMany(b => new[] { Check(b, true), Check(b, false) }));
Console.WriteLine(new JsonArray(rows.ToArray<JsonNode?>()).ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
