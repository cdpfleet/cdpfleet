// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_DE (a German proxy), PROXY_URL_US (a US proxy)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

string[] sites = ["https://www.theguardian.com/international", "https://www.bbc.com/", "https://apnews.com/", "https://www.theverge.com/", "https://www.huffpost.com/"];
// First-layer buttons of the two consent platforms these sites use. OneTrust renders in the
// page; Sourcepoint in an iframe. Only a free "reject" counts: "Reject all and subscribe" doesn't.
var reject = new Dictionary<string, string> { ["OneTrust"] = "#onetrust-reject-all-handler", ["Sourcepoint"] = "button[title=\"Reject all\"], button[title=\"I do not agree\"]" };
var accept = new Dictionary<string, string> { ["OneTrust"] = "#onetrust-accept-btn-handler", ["Sourcepoint"] = "button[title=\"Accept all\"], button[title=\"I agree\"]" };
const string SpFrame = "iframe[id^=\"sp_message_iframe\"]";
const string Banner = "#onetrust-banner-sdk, iframe[id^=\"sp_message_iframe\"]";

// Which consent platform the page runs, whether its banner is showing, and which consent
// APIs it exposes (__tcfapi: IAB Europe's TCF; __gpp: IAB Tech Lab's GPP, used for US state
// privacy laws).
const string Probe = """
    () => {
      const shown = (s) => [...document.querySelectorAll(s)].some((e) => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden');
      const cmp = window.OneTrust || document.querySelector('#onetrust-consent-sdk') ? 'OneTrust'
        : window._sp_ || document.querySelector('iframe[id^="sp_message_iframe"]') ? 'Sourcepoint'
        : window.Didomi ? 'Didomi' : window.Cookiebot ? 'Cookiebot' : window.UC_UI ? 'Usercentrics' : null;
      return { cmp, banner: shown('#onetrust-banner-sdk, iframe[id^="sp_message_iframe"]'),
        tcf: typeof window.__tcfapi === 'function', gpp: typeof window.__gpp === 'function' };
    }
    """;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<IBrowser> Launch(string proxy)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", new { proxy, headless = false });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    return await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
}

static bool OnDomain(string host, string domain) => host == domain || host.EndsWith("." + domain, StringComparison.Ordinal);

// Clicks a consent button; true if the click went through (one retry: a consent iframe that
// re-renders can swallow a click).
static async Task<bool> Press(ILocator button)
{
    for (var attempt = 1; attempt <= 2; attempt++)
    {
        try { await button.ClickAsync(new() { Timeout = 10000 }); return true; }
        catch (Exception) { /* retry once */ }
    }
    return false;
}

// Load a site in a fresh context, then optionally click a consent button. Counts cookies and
// the hosts on other domains the page contacted (ads, analytics, CDNs) up to each moment.
// After is null (and Missing says why) when there was nothing to click.
async Task<(JsonObject Before, JsonObject? After, string? Missing)> Visit(IBrowser browser, string url, string? choice)
{
    var domain = string.Join('.', new Uri(url).Host.Split('.')[^2..]);
    var ctx = await browser.NewContextAsync(new() { ViewportSize = new() { Width = 1280, Height = 900 } });
    var page = await ctx.NewPageAsync();
    var hosts = new HashSet<string>();
    page.Request += (_, r) =>
    {
        var h = Uri.TryCreate(r.Url, UriKind.Absolute, out var u) ? u.Host : "";
        if (h != "" && !OnDomain(h, domain)) lock (hosts) hosts.Add(h);
    };
    async Task<JsonObject> Measure()
    {
        var cookies = await ctx.CookiesAsync();
        int count;
        lock (hosts) count = hosts.Count;
        return new JsonObject
        {
            ["cookies"] = cookies.Count,
            ["third_party_cookies"] = cookies.Count(c => !OnDomain(c.Domain.TrimStart('.'), domain)),
            ["other_hosts"] = count,
        };
    }
    var load = new PageGotoOptions { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 60000 };
    try
    {
        IResponse? res;
        try { res = await page.GotoAsync(url, load); }
        catch (PlaywrightException) { res = await page.GotoAsync(url, load); } // proxies drop a connection now and then: one retry
        // Consent scripts load late on busy pages: wait up to 30 s for the banner, and give
        // every page at least 6 s before measuring.
        var clock = System.Diagnostics.Stopwatch.StartNew();
        long? bannerAfterMs = null;
        try
        {
            await page.Locator(Banner).First.WaitForAsync(new() { State = WaitForSelectorState.Visible, Timeout = 30000 });
            bannerAfterMs = clock.ElapsedMilliseconds;
        }
        catch (Exception) { /* no banner */ }
        if (clock.ElapsedMilliseconds < 6000) await page.WaitForTimeoutAsync(6000 - clock.ElapsedMilliseconds);
        var probe = (JsonObject)Plain(await page.EvaluateAsync<JsonElement>($"({Probe})()"))!;
        var before = new JsonObject { ["status"] = res?.Status };
        foreach (var prop in probe.ToList()) before[prop.Key] = prop.Value?.DeepClone();
        before["banner_after_ms"] = bannerAfterMs;
        foreach (var prop in (await Measure()).ToList()) before[prop.Key] = prop.Value?.DeepClone();
        if (choice is null) return (before, null, null);
        var missing = probe["banner"]!.GetValue<bool>() ? $"no {choice} button on the first layer" : "no banner";
        var cmp = probe["cmp"]?.GetValue<string>();
        if (cmp is null || !(choice == "reject" ? reject : accept).TryGetValue(cmp, out var selector)) return (before, null, missing);
        var button = cmp == "Sourcepoint" ? page.FrameLocator(SpFrame).Locator(selector).First : page.Locator(selector).First;
        try { await button.WaitForAsync(new() { State = WaitForSelectorState.Visible, Timeout = 10000 }); }
        catch (Exception) { return (before, null, missing); }
        var label = await button.GetAttributeAsync("title") ?? (await button.InnerTextAsync()).Trim();
        await page.WaitForTimeoutAsync(2000); // let the banner finish animating in, or the click can be lost
        if (!await Press(button)) return (before, null, "click failed");
        // If the banner is still up, the click was lost: click once more.
        var clicks = 1;
        try { await page.Locator(Banner).First.WaitForAsync(new() { State = WaitForSelectorState.Hidden, Timeout = 5000 }); }
        catch (Exception)
        {
            clicks = 2;
            await Press(button);
        }
        await page.WaitForTimeoutAsync(6000);
        var after = new JsonObject { ["button"] = label, ["clicks"] = clicks };
        foreach (var prop in (await Measure()).ToList()) after[prop.Key] = prop.Value?.DeepClone();
        return (before, after, null);
    }
    finally
    {
        await ctx.CloseAsync();
    }
}

// A site may refuse one exit IP (an error status): visit again in a fresh context, whose new
// proxy connection gets another IP from the rotating proxy.
async Task<(JsonObject Before, JsonObject? After, string? Missing)> VisitOnce(IBrowser browser, string url, string? choice)
{
    var r = await Visit(browser, url, choice);
    var status = r.Before["status"]?.GetValue<int>();
    return status is >= 200 and < 300 ? r : await Visit(browser, url, choice);
}

// Sessions on shared threads end after 2 minutes without traffic by default, so the countries run one after the other.
var fromUs = new List<(JsonObject Before, JsonObject? After, string? Missing)>();
var us = await Launch(Environment.GetEnvironmentVariable("PROXY_URL_US")!);
try
{
    foreach (var url in sites) fromUs.Add(await VisitOnce(us, url, null));
}
finally
{
    await us.CloseAsync();
}
var de = await Launch(Environment.GetEnvironmentVariable("PROXY_URL_DE")!);
try
{
    var results = new JsonArray();
    for (var i = 0; i < sites.Length; i++)
    {
        var url = sites[i];
        var rejected = await VisitOnce(de, url, "reject");
        var accepted = await VisitOnce(de, url, "accept");
        var host = new Uri(url).Host;
        results.Add(new JsonObject
        {
            ["site"] = host.StartsWith("www.") ? host[4..] : host,
            ["us"] = fromUs[i].Before,
            ["de"] = rejected.Before,
            ["de_reject"] = rejected.After,
            ["de_reject_missing"] = rejected.Missing,
            ["de_accept"] = accepted.After,
            ["de_accept_missing"] = accepted.Missing,
        });
    }
    var result = new JsonObject { ["sites"] = results };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await de.CloseAsync();
}

// EvaluateAsync<JsonElement> tags every object with a "$id" reference key; drop those.
static JsonNode? Plain(JsonElement e) => e.ValueKind switch
{
    JsonValueKind.Object => new JsonObject(e.EnumerateObject().Where(p => p.Name != "$id")
        .Select(p => KeyValuePair.Create(p.Name, Plain(p.Value)))),
    JsonValueKind.Array => new JsonArray(e.EnumerateArray().Select(Plain).ToArray()),
    _ => JsonNode.Parse(e.GetRawText()),
};
