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
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chrome/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

const string PageSignals = """
    () => ({
      viewport: `${innerWidth}x${innerHeight}`,
      screen: `${screen.width}x${screen.height}`,
      device_pixel_ratio: devicePixelRatio,
      max_touch_points: navigator.maxTouchPoints,
      coarse_pointer: matchMedia('(pointer: coarse)').matches,
      platform: navigator.platform,
      ua_data_mobile: navigator.userAgentData ? navigator.userAgentData.mobile : null,
      ua_data_platform: navigator.userAgentData ? navigator.userAgentData.platform : null,
    })
    """;

// What a page can see about the device, plus what the network sees (tls.peet.ws).
async Task<JsonObject> Inspect(IBrowserContext context)
{
    var page = await context.NewPageAsync();
    var fp = JsonNode.Parse(await (await page.GotoAsync("https://tls.peet.ws/api/all", new() { Timeout = 60000 }))!.TextAsync())!;
    var js = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(PageSignals)).GetRawText())!.AsObject();
    var headers = fp["http2"]!["sent_frames"]!.AsArray().First(f => (string?)f!["frame_type"] == "HEADERS")!["headers"]!.AsArray()
        .Select(h => (string)h!).ToList();
    string? Header(string name) => headers.FirstOrDefault(h => h.StartsWith(name + ": "))?[(name.Length + 2)..];
    await page.CloseAsync();
    var outp = new JsonObject { ["user_agent"] = fp["user_agent"]!.DeepClone() };
    foreach (var (k, v) in js) outp[k] = v?.DeepClone();
    outp["sec_ch_ua_mobile"] = Header("sec-ch-ua-mobile");
    outp["sec_ch_ua_platform"] = Header("sec-ch-ua-platform");
    outp["ja4"] = fp["tls"]!["ja4"]!.DeepClone();
    outp["akamai_h2_hash"] = fp["http2"]!["akamai_fingerprint_hash"]!.DeepClone();
    return outp;
}

try
{
    var result = new JsonObject
    {
        ["desktop"] = await Inspect(await browser.NewContextAsync()),
        ["iPhone 15 Pro"] = await Inspect(await browser.NewContextAsync(playwright.Devices["iPhone 15 Pro"])),
        ["Pixel 7"] = await Inspect(await browser.NewContextAsync(playwright.Devices["Pixel 7"])),
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
