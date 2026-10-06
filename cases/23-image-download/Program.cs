// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string PageUrl = "https://books.toscrape.com/"; // 20 cover images

// Fetch each image again from inside the page and hand the bytes over as base64.
const string Refetch = """
    urls => Promise.all(urls.map(async (url) => {
      const buf = await (await fetch(url, { cache: 'no-store' })).arrayBuffer();
      let s = ''; const b = new Uint8Array(buf); for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
      return { url, b64: btoa(s) };
    }))
    """;

static bool IsJpeg(byte[] b) => b.Length > 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF;

static JsonObject Summary(string method, List<(string Url, byte[] Bytes)> files, double seconds, int extraRequests) => new()
{
    ["method"] = method,
    ["images"] = files.Count,
    ["valid_jpeg"] = files.Count(f => IsJpeg(f.Bytes)),
    ["total_kb"] = (int)Math.Floor(files.Sum(f => (long)f.Bytes.Length) / 1024.0 + 0.5),
    ["extra_requests"] = extraRequests,
    ["seconds"] = seconds,
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    // Way 1: keep the bytes the page downloads anyway — zero extra requests. The handler
    // only remembers the response; bodies are read after navigation.
    var page = await browser.NewPageAsync();
    var imageResponses = new List<IResponse>();
    page.Response += (_, r) => { if (r.Request.ResourceType == "image" && r.Ok) lock (imageResponses) imageResponses.Add(r); };
    var sw = Stopwatch.StartNew();
    await page.GotoAsync(PageUrl, new() { Timeout = 60000, WaitUntil = WaitUntilState.NetworkIdle });
    var covers = (await page.EvalOnSelectorAllAsync<string[]>("article.product_pod img", "imgs => imgs.map(i => i.currentSrc || i.src)")).ToList();
    var fromLoad = new List<(string, byte[])>();
    IResponse[] snapshot; lock (imageResponses) snapshot = imageResponses.ToArray();
    foreach (var r in snapshot)
    {
        if (!covers.Contains(r.Url)) continue;
        try { fromLoad.Add((r.Url, await r.BodyAsync())); } catch (PlaywrightException) { /* body gone (cache) */ }
    }
    var way1 = Summary("capture responses while the page loads", fromLoad, sw.ElapsedMilliseconds / 1000.0, 0);

    // Way 2: fetch each image again from inside the page (same cookies, proxy and headers).
    sw.Restart();
    var again = await page.EvaluateAsync<JsonElement>(Refetch, covers);
    var refetched = again.EnumerateArray().Select(a => (a.GetProperty("url").GetString()!, Convert.FromBase64String(a.GetProperty("b64").GetString()!))).ToList();
    var way2 = Summary("fetch() each image again in the page", refetched, sw.ElapsedMilliseconds / 1000.0, covers.Count);

    var output = new JsonArray(way1, way2);
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
