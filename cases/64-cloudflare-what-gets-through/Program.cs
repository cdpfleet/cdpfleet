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

// Sticky residential exits: every setup in a comparison uses the same session name, so the same exit IP.
var networks = new Dictionary<string, (string Label, string? Proxy)>
{
    ["own"] = ("own proxy (rotating)", Environment.GetEnvironmentVariable("PROXY_URL")),
    ["tier1"] = ("cdpfleet-resi Tier 1, sticky US", $"cdpfleet-resi-country-us-session-c64{run}-lifetime-30"),
    ["tier4"] = ("cdpfleet-resi Tier 4, sticky US", $"cdpfleet-resi-tier-4-country-us-session-c64{run}-lifetime-30"),
};
var setups = new Dictionary<string, (string Name, string Engine, IBrowserType Type, bool Headless)>
{
    ["chromium"] = ("Chromium, headless", "chromium", playwright.Chromium, true),
    ["chrome"] = ("Google Chrome, headful", "chrome", playwright.Chromium, false),
    ["edge"] = ("Microsoft Edge, headful", "edge", playwright.Chromium, false),
    ["camoufox"] = ("Camoufox, defaults", "camoufox", playwright.Firefox, true),
};
// Public test pages only: two Cloudflare challenge pages and a Turnstile test page in two widget modes.
// "cf" pages may answer with Cloudflare's interstitial; "widget" pages embed Turnstile and get a token if it passes.
var pages = new (string Key, string Url, string Kind)[]
{
    ("nowsecure", "https://nowsecure.nl/", "cf"),
    ("scrapingcourse", "https://www.scrapingcourse.com/cloudflare-challenge", "cf"),
    ("ts_noninteractive", "https://peet.ws/turnstile-test/non-interactive.html", "widget"),
    ("ts_managed", "https://peet.ws/turnstile-test/managed.html", "widget"),
};

// What the page shows right now, in the lab's words. The same expression runs in every language.
const string State = """
(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  const el = document.querySelector('[name="cf-turnstile-response"]');
  const state = /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
  return { state, token: el && el.value ? el.value.length : 0 };
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

async Task<bool> HasClearance(IBrowserContext context) => (await context.CookiesAsync()).Any(c => c.Name == "cf_clearance");

// One load, then watch the page once a second without touching it: an interstitial gets 20 s to
// clear by itself, a Turnstile widget 12 s to issue a token. A network failure is retried once.
async Task<JsonObject> Visit(IBrowserContext context, string pageKey)
{
    var page = pages.First(p => p.Key == pageKey);
    var tab = await context.NewPageAsync();
    var clock = Stopwatch.StartNew();
    var cell = new JsonObject { ["outcome"] = "challenge", ["seconds"] = null };
    var nav = new PageGotoOptions { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 };
    try
    {
        try { await tab.GotoAsync(page.Url, nav); }
        catch (PlaywrightException) { await tab.GotoAsync(page.Url, nav); }
    }
    catch (Exception)
    {
        await tab.CloseAsync();
        return new JsonObject { ["outcome"] = "error", ["seconds"] = null };
    }
    var sawChallenge = false;
    var limit = page.Kind == "widget" ? 12000 : 20000;
    while (clock.ElapsedMilliseconds < limit)
    {
        string? state = null;
        var token = 0;
        try
        {
            var s = await tab.EvaluateAsync<JsonElement>(State);
            state = s.GetProperty("state").GetString();
            token = s.GetProperty("token").GetInt32();
        }
        catch (PlaywrightException) { } // while the interstitial reloads the page
        var seconds = (int)Math.Round(clock.ElapsedMilliseconds / 1000.0);
        if (state == "blocked") { cell["outcome"] = "blocked"; break; }
        if (state == "challenge") sawChallenge = true;
        if (state != null && page.Kind == "widget" && token > 0) { cell["outcome"] = "cleared"; cell["seconds"] = seconds; break; }
        if (state == "page" && page.Kind == "cf")
        {
            cell["outcome"] = sawChallenge ? "cleared" : "content";
            if (sawChallenge) cell["seconds"] = seconds;
            break;
        }
        await Task.Delay(1000);
    }
    if (page.Kind == "cf")
    {
        // The clearance cookie can arrive a moment after the content: give it up to 6 s.
        for (var i = 0; i < 6 && (string?)cell["outcome"] != "blocked" && !await HasClearance(context); i++) await Task.Delay(1000);
        cell["cf_clearance"] = await HasClearance(context);
    }
    else
    {
        cell["token"] = (string?)cell["outcome"] == "cleared";
    }
    await tab.CloseAsync();
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
    var name = $"case64-{run}";
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

var matrix = new JsonArray();
foreach (var network in new[] { "own", "tier1" })
    foreach (var setupKey in new[] { "chromium", "chrome", "edge", "camoufox" })
        matrix.Add(await Row(setupKey, network, pages.Select(p => p.Key).ToArray()));
var tier4 = new JsonArray();
foreach (var setupKey in new[] { "chrome", "camoufox" }) tier4.Add(await Row(setupKey, "tier4", new[] { "nowsecure", "scrapingcourse", "ts_managed" }));
var profile = await Returning("camoufox", "tier1", "scrapingcourse");

var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["matrix"] = matrix, ["tier4"] = tier4, ["profile"] = profile };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
