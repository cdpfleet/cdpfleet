// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (any exit), PROXY_URL_DE (an exit in Germany)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// A German Windows desktop: every value below is part of one consistent story.
object Persona(string proxy) => new
{
    proxy,
    headless = true,
    os = "windows",
    locale = "de-DE",
    screen = new { minWidth = 1920, maxWidth = 1920, minHeight = 1080, maxHeight = 1080 },
    window = new[] { 1600, 900 },
    humanize = true,
    block_webrtc = true,
    geoip = true, // timezone and geolocation follow the proxy's exit IP
};

const string PageSignals = """
    () => {
      const gl = document.createElement('canvas').getContext('webgl');
      const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
      return {
        platform: navigator.platform,
        languages: navigator.languages,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        screen: `${screen.width}x${screen.height}`,
        window: `${outerWidth}x${outerHeight}`,
        hardware_concurrency: navigator.hardwareConcurrency,
        webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
        webrtc: typeof RTCPeerConnection !== 'undefined',
      };
    }
    """;

async Task<JsonObject> Run(string proxy)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/camoufox/session", Persona(proxy));
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Firefox.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        var fp = JsonNode.Parse(await (await page.GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 60000 }))!.TextAsync())!;
        var seen = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(PageSignals)).GetRawText())!.AsObject();
        var headers = fp["http2"]!["sent_frames"]!.AsArray().First(f => (string?)f!["frame_type"] == "HEADERS")!["headers"]!.AsArray();
        // Where the proxy exits, as a website would look it up.
        var geo = JsonNode.Parse(await (await page.GotoAsync("http://ip-api.com/json/?fields=country,timezone", new() { Timeout = 60000 }))!.TextAsync())!;
        var outp = new JsonObject
        {
            ["exit_country"] = geo["country"]!.DeepClone(),
            ["exit_timezone"] = geo["timezone"]!.DeepClone(),
            ["user_agent"] = fp["user_agent"]!.DeepClone(),
            ["accept_language"] = headers.Select(h => (string)h!).FirstOrDefault(h => h.StartsWith("accept-language: "))?[17..],
        };
        foreach (var (k, v) in seen) outp[k] = v?.DeepClone();
        outp["ja4"] = fp["tls"]!["ja4"]!.DeepClone();
        return outp;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var result = new JsonObject
{
    ["random exit"] = await Run(Environment.GetEnvironmentVariable("PROXY_URL")!),
    ["German exit"] = await Run(Environment.GetEnvironmentVariable("PROXY_URL_DE")!),
};
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
