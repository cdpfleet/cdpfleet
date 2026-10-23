// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var run = Convert.ToHexString(RandomNumberGenerator.GetBytes(3)).ToLowerInvariant();
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
var networks = new Dictionary<string, (string Label, string? Proxy)>
{
    ["own"] = ("own proxy (rotating)", Environment.GetEnvironmentVariable("PROXY_URL")),
    ["tier1"] = ("cdpfleet-resi Tier 1, sticky US", $"cdpfleet-resi-country-us-session-c67{run}-lifetime-30"),
    ["tier4"] = ("cdpfleet-resi Tier 4, sticky US", $"cdpfleet-resi-tier-4-country-us-session-c67{run}-lifetime-30"),
};
var setups = new Dictionary<string, (string Name, string Engine, IBrowserType Type, bool Headless)>
{
    ["chromium"] = ("Chromium, headless", "chromium", playwright.Chromium, true),
    ["chrome"] = ("Google Chrome, headful", "chrome", playwright.Chromium, false),
    ["edge"] = ("Microsoft Edge, headful", "edge", playwright.Chromium, false),
    ["camoufox"] = ("Camoufox, defaults", "camoufox", playwright.Firefox, true),
};
// Each vendor's own home page, which that vendor protects with its own product.
var pxHost = new Regex(@"(^|\.)px-(cloud|cdn)\.net$");
var pages = new (string Key, string Url, Func<string, bool> Sensor)[]
{
    ("akamai", "https://www.akamai.com/", h => h == "www.akamai.com"),
    ("human", "https://www.humansecurity.com/", h => pxHost.IsMatch(h)),
};

// What the page shows right now, in the lab's words. The same expression runs in every language.
const string State = """
(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const blocked = /access denied|you don't have permission to access|request blocked|reference #\d/i.test(t);
  const challenge = !!document.querySelector('#px-captcha, #sec-if-cpt-container, #sec-cpt-if') || /press (&|and) hold|challenge validation/i.test(t);
  return blocked ? 'blocked' : challenge ? 'challenge' : 'page';
})()
""";

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<IBrowser> Launch(string setupKey, string network, string? profile = null)
{
    var s = setups[setupKey];
    var body = new JsonObject { ["proxy"] = networks[network].Proxy, ["headless"] = s.Headless, ["record"] = false };
    if (profile != null) body["profile"] = profile;
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{s.Engine}/session", body);
        if (res.IsSuccessStatusCode)
        {
            var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
            return await s.Type.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
        }
        var code = (int)res.StatusCode;
        if ((code != 429 && code != 503) || attempt == 5) throw new Exception($"launch {code} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5));
    }
}

// Akamai's _abck carries a status between its first two "~": -1 until a sensor post is accepted. Only that
// field is kept, never the cookie itself. HUMAN issues _px3 once its collector accepts the browser.
async Task<(string? Abck, bool Px3)> Signals(IBrowserContext context)
{
    var jar = await context.CookiesAsync();
    var abck = jar.FirstOrDefault(c => c.Name == "_abck")?.Value;
    var parts = abck?.Split('~');
    return (parts == null ? null : parts.Length > 1 ? parts[1] : "?", jar.Any(c => c.Name == "_px3"));
}

async Task<string?> PageState(IPage tab)
{
    try { return await tab.EvaluateAsync<string>(State); }
    catch (PlaywrightException) { return null; } // while the page navigates
}

// One load, then watch the page once a second for 10 s without touching it, so the vendor's script
// can post its sensor data; a network failure is retried once.
async Task<JsonObject> Visit(IBrowserContext context, string pageKey)
{
    var page = pages.First(p => p.Key == pageKey);
    var tab = await context.NewPageAsync();
    var posts = 0;
    tab.Request += (_, r) =>
    {
        if (r.Method == "POST" && Uri.TryCreate(r.Url, UriKind.Absolute, out var u) && page.Sensor(u.Host)) Interlocked.Increment(ref posts);
    };
    var nav = new PageGotoOptions { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 };
    IResponse? res;
    try
    {
        try { res = await tab.GotoAsync(page.Url, nav); }
        catch (PlaywrightException) { res = await tab.GotoAsync(page.Url, nav); }
    }
    catch (Exception)
    {
        await tab.CloseAsync();
        return new JsonObject { ["outcome"] = "error" };
    }
    var atLoad = await Signals(context);
    var seen = new HashSet<string>();
    for (var i = 0; i < 10; i++)
    {
        var s = await PageState(tab);
        if (s != null) seen.Add(s);
        if (s == "blocked") break;
        await Task.Delay(1000);
    }
    var last = await PageState(tab);
    var outcome = seen.Contains("blocked") || last == "blocked" ? "blocked"
        : last == "challenge" ? "challenge" : seen.Contains("challenge") ? "cleared" : "content";
    var after = await Signals(context);
    await tab.CloseAsync();
    var cell = new JsonObject { ["outcome"] = outcome, ["status"] = res?.Status, ["sensor_posts"] = posts };
    if (pageKey == "akamai") { cell["abck_at_load"] = atLoad.Abck; cell["abck_after"] = after.Abck; }
    else cell["px3"] = after.Px3;
    return cell;
}

// One session per setup and network; a fresh context per page so every load is a first visit.
async Task<JsonObject> Row(string setupKey, string network, string[] keys)
{
    var browser = await Launch(setupKey, network);
    var row = new JsonObject { ["setup"] = setups[setupKey].Name, ["network"] = networks[network].Label };
    try
    {
        foreach (var k in keys)
        {
            var context = await browser.NewContextAsync();
            row[k] = await Visit(context, k);
            await context.CloseAsync();
        }
    }
    finally
    {
        await browser.CloseAsync();
    }
    return row;
}

async Task<JsonElement?> ProfileInfo(string name)
{
    var body = await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me/profiles");
    if (!body.TryGetProperty("profiles", out var list)) return null;
    foreach (var p in list.EnumerateArray()) if (p.GetProperty("name").GetString() == name) return p;
    return null;
}

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart.
async Task<JsonObject> Returning(string setupKey, string network, string pageKey)
{
    var name = $"case67-{run}";
    var visits = new List<JsonObject>();
    for (var i = 0; i < 2; i++)
    {
        var browser = await Launch(setupKey, network, name);
        try
        {
            visits.Add(await Visit(browser.Contexts[0], pageKey)); // the profile's own context; a NewContextAsync() is not saved
        }
        finally
        {
            await browser.CloseAsync();
        }
        // The node saves the profile after the browser exits: wait until it is free again.
        for (var j = 0; j < 60; j++)
        {
            var info = await ProfileInfo(name);
            if (info is { } p && !p.GetProperty("in_use").GetBoolean()) break;
            await Task.Delay(2000);
        }
    }
    await http.DeleteAsync($"https://cdpfleet.com/v1/me/profiles/{name}");
    return new JsonObject
    {
        ["setup"] = setups[setupKey].Name, ["network"] = networks[network].Label, ["page"] = pageKey,
        ["first"] = visits[0], ["second"] = visits[1],
    };
}

var keys = pages.Select(p => p.Key).ToArray();
var matrix = new JsonArray();
foreach (var network in new[] { "own", "tier1" })
    foreach (var setupKey in new[] { "chromium", "chrome", "edge", "camoufox" })
        matrix.Add(await Row(setupKey, network, keys));
var tier4 = new JsonArray();
foreach (var setupKey in new[] { "chrome", "camoufox" }) tier4.Add(await Row(setupKey, "tier4", keys));
var profile = await Returning("chrome", "tier1", "akamai");

var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["matrix"] = matrix, ["tier4"] = tier4, ["profile"] = profile };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
