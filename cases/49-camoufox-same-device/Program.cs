// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var name = $"case49-csharp-{Convert.ToHexString(RandomNumberGenerator.GetBytes(4)).ToLower()}"; // one profile per run
const string Site = "https://httpbin.org/html";
// One country for every launch: Camoufox matches timezone and locale to the exit IP.
JsonObject Body() => new() { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL_US"), ["headless"] = true, ["os"] = "windows", ["record"] = false };

const string Fingerprint = @"() => {
  const gl = document.createElement('canvas').getContext('webgl');
  const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
  const draw = () => {
    const c = document.createElement('canvas');
    c.width = 240; c.height = 60;
    const ctx = c.getContext('2d');
    ctx.font = '18px Arial'; ctx.fillStyle = '#f60'; ctx.fillRect(10, 10, 100, 30); ctx.fillStyle = '#069'; ctx.fillText('cdpfleet device 49', 4, 40);
    return c.toDataURL();
  };
  return {
    user_agent: navigator.userAgent, platform: navigator.platform, cores: navigator.hardwareConcurrency,
    screen: `${screen.width}x${screen.height}`, webgl_renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : null,
    canvas: draw(), canvas_again: draw(), timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, languages: navigator.languages,
    login_cookie: document.cookie.includes('session=alice-7f3'), login_storage: localStorage.getItem('user') === 'alice',
  };
}";

string ShortHash(string text, int n) => Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(text))).ToLower()[..n];

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// The node saves the profile after the session's browser has exited: wait for the new version.
async Task WaitForVersion(int version)
{
    for (var i = 0; i < 60; i++)
    {
        var profiles = (await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me/profiles")).GetProperty("profiles");
        foreach (var p in profiles.EnumerateArray())
            if (p.GetProperty("name").GetString() == name && p.GetProperty("version").GetInt32() >= version && !p.GetProperty("in_use").GetBoolean()) return;
        await Task.Delay(2000);
    }
    throw new Exception($"profile {name} never reached version {version}");
}

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

// What a fingerprinting script reads, plus the "login" kept in a cookie and localStorage.
async Task<JsonObject> Visit(IPage page, bool logIn)
{
    await Go(page, Site);
    if (logIn) await page.EvaluateAsync("() => { document.cookie = 'session=alice-7f3; max-age=604800; path=/'; localStorage.setItem('user', 'alice'); }");
    var fp = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(Fingerprint)).GetRawText())!.AsObject();
    var canvas = fp["canvas"]!.GetValue<string>();
    fp["canvas_same_in_session"] = canvas == fp["canvas_again"]!.GetValue<string>();
    fp["canvas"] = ShortHash(canvas, 12);
    fp.Remove("canvas_again");
    return fp;
}

async Task<JsonObject> Session(JsonObject body, bool logIn)
{
    var s = await Launch(body);
    var browser = await playwright.Firefox.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        // With a profile, its context is browser.Contexts[0]; without one, a fresh context.
        var context = body.ContainsKey("profile") ? browser.Contexts[0] : await browser.NewContextAsync();
        var page = context.Pages.Count > 0 ? context.Pages[0] : await context.NewPageAsync();
        return await Visit(page, logIn);
    }
    finally
    {
        await browser.CloseAsync();
    }
}

// The device's hardware-like values (user agent, platform, cores, screen, GPU); the canvas is
// reported on its own. Compact JSON like JavaScript's JSON.stringify, so hashes match.
string Device(JsonObject r) => ShortHash(new JsonArray(r["user_agent"]!.DeepClone(), r["platform"]!.DeepClone(), r["cores"]!.DeepClone(), r["screen"]!.DeepClone(), r["webgl_renderer"]?.DeepClone())
    .ToJsonString(new JsonSerializerOptions { Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }), 10);

JsonObject Pick(int run, JsonObject r)
{
    var o = new JsonObject { ["run"] = run, ["device"] = Device(r) };
    foreach (var k in new[] { "user_agent", "cores", "screen", "webgl_renderer", "canvas", "canvas_same_in_session", "timezone", "login_cookie", "login_storage" }) o[k] = r[k]?.DeepClone();
    return o;
}

try
{
    // Four launches with one profile: the first creates it (device + login), the rest restore it.
    var withProfile = new JsonArray();
    for (var run = 1; run <= 4; run++)
    {
        var body = Body();
        body["profile"] = name;
        withProfile.Add(Pick(run, await Session(body, run == 1)));
        await WaitForVersion(run);
    }
    // Two launches without a profile, for contrast.
    var without = new JsonArray();
    for (var run = 1; run <= 2; run++) without.Add(Pick(run, await Session(Body(), false)));
    Console.WriteLine(new JsonObject { ["with_profile"] = withProfile, ["without_profile"] = without }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await http.DeleteAsync($"https://cdpfleet.com/v1/me/profiles/{name}");
}
