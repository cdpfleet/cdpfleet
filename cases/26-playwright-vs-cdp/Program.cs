// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// What a page can notice about the client driving it. A debugger that has Runtime.enable'd
// the page serializes logged errors, which reads their `stack` getter.
const string Probe = """
    (async () => {
      let stackRead = false;
      const e = new Error('probe');
      Object.defineProperty(e, 'stack', { get() { stackRead = true; return ''; } });
      console.debug(e);
      await new Promise((r) => setTimeout(r, 100));
      return { stack_read_by_debugger: stackRead, webdriver: navigator.webdriver };
    })()
    """;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);

async Task<JsonElement> Launch(bool cdp)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chrome/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new", cdp });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    return await res.Content.ReadFromJsonAsync<JsonElement>();
}

using var playwright = await Playwright.CreateAsync();
var headers = new Dictionary<string, string> { ["x-api-key"] = key };

async Task<JsonObject> Measure(string mode)
{
    var pwProtocol = mode == "playwright protocol";
    var s = await Launch(!pwProtocol);
    var sw = Stopwatch.StartNew();
    var browser = pwProtocol
        ? await playwright.Chromium.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new BrowserTypeConnectOptions { Headers = headers })
        : await playwright.Chromium.ConnectOverCDPAsync(s.GetProperty("cdpUrl").GetString()!, new BrowserTypeConnectOverCDPOptions { Headers = headers });
    var connectMs = sw.ElapsedMilliseconds;
    try
    {
        var page = await browser.NewPageAsync();
        var logged = new List<string>();
        page.Console += (_, m) => { lock (logged) logged.Add(m.Type); };
        for (var attempt = 1; ; attempt++) // the proxy can drop a tunnel; retry
        {
            try { await page.GotoAsync("https://example.com/", new() { Timeout = 60000 }); break; }
            catch (Exception e) when (e is PlaywrightException or TimeoutException) { if (attempt == 3) throw; }
        }
        var seen = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(Probe)).GetRawText())!;
        await page.WaitForTimeoutAsync(200); // let the console event arrive
        bool pdf;
        try { pdf = (await page.PdfAsync()).Length > 0; }
        catch (PlaywrightException) { pdf = false; } // not over this connection
        bool sawDebug;
        lock (logged) sawDebug = logged.Contains("debug");
        return new JsonObject
        {
            ["mode"] = mode,
            ["endpoint"] = pwProtocol ? "wsUrl" : "cdpUrl",
            ["client_version_must_match"] = pwProtocol,
            ["connect_ms"] = connectMs,
            ["browser_version"] = browser.Version,
            ["console_events"] = sawDebug,
            ["pdf"] = pdf,
            ["stack_read_by_debugger"] = seen["stack_read_by_debugger"]?.DeepClone(),
            ["webdriver"] = seen["webdriver"]?.DeepClone(),
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var output = new JsonArray { await Measure("playwright protocol"), await Measure("connectOverCDP") };
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
