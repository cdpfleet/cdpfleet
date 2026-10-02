// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL, PROXY_URL_DE, SOCKS_PROXIES (comma-separated socks5:// URLs)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Echo = "wss://ws.postman-echo.com/raw"; // a public WebSocket echo server
var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxies = new (string Label, string Proxy)[]
{
    ("residential (any country)", Environment.GetEnvironmentVariable("PROXY_URL")!),
    ("residential (Germany)", Environment.GetEnvironmentVariable("PROXY_URL_DE")!),
    ("datacenter SOCKS5", Environment.GetEnvironmentVariable("SOCKS_PROXIES")!.Split(',')[0]),
};

// Runs in the page: connect, then 20 echo round trips, one at a time.
const string Probe = """
    async (url) => {
      const t0 = performance.now();
      const ws = new WebSocket(url);
      await new Promise((ok, fail) => { ws.onopen = ok; ws.onerror = () => fail(new Error('WebSocket failed')); });
      const connectMs = performance.now() - t0;
      const rtts = [];
      for (let i = 0; i < 20; i++) {
        const sent = performance.now();
        const echoed = new Promise((ok) => { ws.onmessage = (m) => ok(m.data); });
        ws.send(`ping ${i}`);
        if ((await echoed) !== `ping ${i}`) throw new Error('wrong echo');
        rtts.push(performance.now() - sent);
      }
      ws.close();
      rtts.sort((a, b) => a - b);
      return { connectMs, p50: rtts[10], p95: rtts[18], min: rtts[0] };
    }
    """;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> MeasureOnce(string label, string proxy)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", new { proxy, headless = true });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode}");
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        // Where this proxy exits (also gives the page an https origin to open the socket from).
        var geo = JsonNode.Parse(await (await page.GotoAsync("http://ip-api.com/json/?fields=country,city", new() { Timeout = 30000 }))!.TextAsync())!;
        await page.GotoAsync("https://httpbin.org/html", new() { Timeout = 30000 });
        var r = await page.EvaluateAsync<JsonElement>(Probe, Echo);
        return new JsonObject
        {
            ["proxy"] = label, ["exit"] = $"{geo["city"]}, {geo["country"]}",
            ["connect_ms"] = Math.Round(r.GetProperty("connectMs").GetDouble()),
            ["rtt_p50_ms"] = Math.Round(r.GetProperty("p50").GetDouble()),
            ["rtt_p95_ms"] = Math.Round(r.GetProperty("p95").GetDouble()),
            ["rtt_min_ms"] = Math.Round(r.GetProperty("min").GetDouble()),
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

// Residential exits drop a connection now and then: one retry in a new session.
async Task<JsonObject> Measure(string label, string proxy)
{
    for (var attempt = 1; ; attempt++)
    {
        try { return await MeasureOnce(label, proxy); }
        catch (Exception err) when (attempt < 2) { _ = err; }
        catch (Exception err) { return new JsonObject { ["proxy"] = label, ["error"] = err.Message.Split('\n')[0] }; }
    }
}

var rows = new JsonArray();
foreach (var (label, proxy) in proxies) rows.Add(await Measure(label, proxy));
var result = new JsonObject { ["echo_server"] = Echo, ["round_trips"] = 20, ["results"] = rows };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
