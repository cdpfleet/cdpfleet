// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
// Reports the caller's IP, user agent, HTTP version and TLS fingerprint (JA4).
const string Peet = "https://tls.peet.ws/api/all";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var session = await res.Content.ReadFromJsonAsync<JsonElement>();

// A plain client for third-party URLs: no API key, no proxy, the default (empty) user agent.
using var plain = new HttpClient();

async Task<JsonObject> Row(string path, JsonNode seen, string runsOn)
{
    var ip = seen["ip"]!.GetValue<string>().Split(':')[0];
    // Who owns the exit: a residential ISP (your proxy) or a hosting provider (a server)?
    var who = JsonNode.Parse(await plain.GetStringAsync($"http://ip-api.com/json/{ip}?fields=hosting"))!;
    var tls = seen["tls"]!;
    var extensions = tls["extensions"]!.AsArray();
    return new JsonObject
    {
        ["path"] = path,
        ["runs_on"] = runsOn,
        ["exit_ip"] = ip,
        ["exit_type"] = who["hosting"]?.GetValue<bool>() == true ? "datacenter" : "residential",
        ["user_agent"] = seen["user_agent"]?.DeepClone(),
        ["http_version"] = seen["http_version"]?.DeepClone(),
        ["ja4"] = tls["ja4"]?.DeepClone(),
        ["tls_extensions"] = extensions.Count,
        ["resumed_tls"] = extensions.Any(e => (e?["name"]?.GetValue<string>() ?? "").Contains("pre_shared_key")),
    };
}

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(session.GetProperty("wsUrl").GetString()!,
    new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync();
    // 1. A navigation: the browser itself makes the request.
    var nav = JsonNode.Parse(await (await page.GotoAsync(Peet, new() { Timeout = 60000 }))!.TextAsync())!;
    // 2. fetch() inside the page (same origin): the browser's network stack, cookies and headers.
    var inPage = JsonNode.Parse((await page.EvaluateAsync<JsonElement>("() => fetch('/api/all').then((r) => r.json())")).GetRawText())!;
    // 3. page.APIRequest: Playwright's own HTTP client, run by the Playwright server next to the browser.
    var viaRequest = JsonNode.Parse(await (await page.APIRequest.GetAsync(Peet, new() { Timeout = 60000 })).TextAsync())!;
    // 4. Your own HTTP client on your machine, for reference.
    var local = JsonNode.Parse(await plain.GetStringAsync(Peet))!;

    var output = new JsonArray
    {
        await Row("page.goto", nav, "the browser"),
        await Row("fetch() in page.evaluate", inPage, "the browser"),
        await Row("page.request.get", viaRequest, "Playwright server"),
        await Row("fetch() in your script", local, "your machine"),
    };
    // JA4's middle part hashes the cipher suites: the same TLS stack keeps it across connections.
    var browserCiphers = output[0]!["ja4"]!.GetValue<string>().Split('_')[1];
    foreach (var r in output)
        r!["same_tls_stack_as_browser"] = r["ja4"]!.GetValue<string>().Split('_')[1] == browserCiphers;
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
