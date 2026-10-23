// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (one page load per page per setup and network; nothing is clicked or solved)
using System.Diagnostics;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var run = Convert.ToHexString(RandomNumberGenerator.GetBytes(3)).ToLowerInvariant();
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// Every setup gets its own sticky residential exit (letter = setup): DataDome scores the IP as well as the browser,
// so one setup's visits must not change what the next one sees.
var networks = new Dictionary<string, string>
{
    ["own"] = "own proxy (rotating)",
    ["tier1"] = "cdpfleet-resi Tier 1, sticky US",
    ["tier4"] = "cdpfleet-resi Tier 4, sticky US",
};
string? Proxy(string network, string exit) => network switch
{
    "tier1" => $"cdpfleet-resi-country-us-session-d{run}{exit}-lifetime-30",
    "tier4" => $"cdpfleet-resi-tier-4-country-us-session-d{run}{exit}-lifetime-30",
    _ => Environment.GetEnvironmentVariable("PROXY_URL"),
};
var setups = new Dictionary<string, (string Id, string Name, string Engine, IBrowserType Type, bool Headless)>
{
    ["chromium"] = ("a", "Chromium, headless", "chromium", playwright.Chromium, true),
    ["chrome"] = ("b", "Google Chrome, headful", "chrome", playwright.Chromium, false),
    ["edge"] = ("c", "Microsoft Edge, headful", "edge", playwright.Chromium, false),
    ["camoufox"] = ("d", "Camoufox, defaults", "camoufox", playwright.Firefox, true),
};
// DataDome's own public site, which DataDome protects: its home page and its pricing page.
var pages = new Dictionary<string, string>
{
    ["home"] = "https://datadome.co/",
    ["pricing"] = "https://datadome.co/pricing/",
};

// What the page shows right now, in the lab's words. DataDome answers an intercepted request with a 403 page that
// embeds an iframe from captcha-delivery.com: a device check that passes by itself, a CAPTCHA, or a block (t=bv).
// The same expression runs in every language.
const string State = """
(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()
""";

// 503 means the fleet is momentarily busy: wait Retry-After and try again. A profile's second session can land on a
// node with an older build of the browser (409 profile_version_downgrade): ask again for another node.
async Task<IBrowser> Launch(string setupKey, string network, string? exit = null, JsonObject? extra = null)
{
    var s = setups[setupKey];
    var body = new JsonObject { ["proxy"] = Proxy(network, exit ?? s.Id), ["headless"] = s.Headless, ["record"] = false };
    if (extra != null) foreach (var (k, v) in extra) body[k] = v?.DeepClone();
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{s.Engine}/session", body);
        if (res.IsSuccessStatusCode)
        {
            var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
            return await s.Type.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
        }
        var code = (int)res.StatusCode;
        var text = await res.Content.ReadAsStringAsync();
        var retry = code is 429 or 503 || (code == 409 && text.Contains("profile_version_downgrade"));
        if (!retry || attempt == 8) throw new Exception($"launch {code} {text}");
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5));
    }
}

JsonObject Cell(string outcome, int? status, int? seconds) => new() { ["outcome"] = outcome, ["status"] = status, ["seconds"] = seconds };

async Task<int?> GoTo(IPage tab, string url) =>
    (await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 }))?.Status;

// Load a page (a network failure is retried once), then watch it once a second for 12 s without touching it.
// content = the page straight away; cleared = DataDome intercepted (403 or its device check) and let the browser
// through by itself; challenge = a CAPTCHA (never solved); blocked = DataDome's block page.
async Task<JsonObject> Visit(IPage tab, string url)
{
    var clock = Stopwatch.StartNew();
    int? status;
    try
    {
        try { status = await GoTo(tab, url); }
        catch (PlaywrightException) { status = await GoTo(tab, url); }
    }
    catch (Exception)
    {
        return Cell("error", null, null);
    }
    var checkedByDataDome = status == 403;
    var until = clock.ElapsedMilliseconds + 12000; // the watch starts once the page has loaded, however slow the proxy was
    while (clock.ElapsedMilliseconds < until)
    {
        string? s = null;
        try { s = await tab.EvaluateAsync<string>(State); }
        catch (PlaywrightException) { } // while the device check reloads the page
        if (s is "blocked" or "challenge") return Cell(s, status, null);
        if (s == "checking") checkedByDataDome = true;
        if (s == "page")
            return checkedByDataDome ? Cell("cleared", status, (int)Math.Round(clock.ElapsedMilliseconds / 1000.0)) : Cell("content", status, null);
        await Task.Delay(1000);
    }
    return Cell(checkedByDataDome ? "challenge" : "error", status, null);
}

// One session per setup and network; a fresh context per page so every load is a first visit.
async Task<JsonObject> Row(string setupKey, string network, string[] keys)
{
    var browser = await Launch(setupKey, network);
    var row = new JsonObject { ["setup"] = setups[setupKey].Name, ["network"] = networks[network] };
    try
    {
        foreach (var k in keys)
        {
            var context = await browser.NewContextAsync();
            row[k] = await Visit(await context.NewPageAsync(), pages[k]);
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

async Task<bool> HasCookie(IBrowserContext context) => (await context.CookiesAsync()).Any(c => c.Name == "datadome");

// A returning visitor: the same profile and the same sticky exit, two sessions a few seconds apart. The profile keeps
// the datadome cookie the first visit was given.
async Task<JsonObject> Returning(string setupKey)
{
    var s = setups[setupKey];
    var name = $"case65-{run}-{s.Id}";
    var visits = new List<JsonObject>();
    for (var i = 0; i < 2; i++)
    {
        var browser = await Launch(setupKey, "tier1", $"{s.Id}p", new JsonObject { ["profile"] = name });
        try
        {
            var context = browser.Contexts[0]; // the profile's own context; a NewContextAsync() is not saved
            var cell = await Visit(await context.NewPageAsync(), pages["home"]);
            // DataDome's script sets its cookie a moment after the page: give it up to 5 s.
            for (var j = 0; j < 5 && !await HasCookie(context); j++) await Task.Delay(1000);
            cell["cookie"] = await HasCookie(context);
            visits.Add(cell);
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
    return new JsonObject { ["setup"] = s.Name, ["network"] = networks["tier1"], ["first"] = visits[0], ["second"] = visits[1] };
}

// Camoufox with humanize on and off: two page views in one context, with three mouse moves between them, so DataDome's
// script on the first page can see how the cursor moves before the second page is requested.
async Task<JsonObject> Humanize(bool on)
{
    var browser = await Launch("camoufox", "tier1", on ? "h1" : "h0", new JsonObject { ["humanize"] = on });
    try
    {
        var tab = await (await browser.NewContextAsync()).NewPageAsync();
        var row = new JsonObject { ["setup"] = $"Camoufox, humanize {(on ? "on" : "off")}", ["network"] = networks["tier1"] };
        row["home"] = await Visit(tab, pages["home"]);
        var clock = Stopwatch.StartNew();
        foreach (var (x, y) in new[] { (200, 300), (700, 250), (450, 600) }) await tab.Mouse.MoveAsync(x, y);
        row["move_seconds"] = Math.Round(clock.ElapsedMilliseconds / 100.0) / 10.0;
        row["pricing"] = await Visit(tab, pages["pricing"]);
        return row;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var matrix = new JsonArray();
foreach (var network in new[] { "own", "tier1" })
    foreach (var setupKey in new[] { "chromium", "chrome", "edge", "camoufox" })
        matrix.Add(await Row(setupKey, network, pages.Keys.ToArray()));
var tier4 = new JsonArray();
foreach (var setupKey in new[] { "chrome", "camoufox" }) tier4.Add(await Row(setupKey, "tier4", new[] { "home" }));
var profiles = new JsonArray { await Returning("chrome"), await Returning("camoufox") };
var human = new JsonArray { await Humanize(true), await Humanize(false) };

var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["matrix"] = matrix, ["tier4"] = tier4, ["profiles"] = profiles, ["humanize"] = human };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
