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

// The sticky residential exit is shared by every setup, so a difference between rows is the browser.
var networks = new Dictionary<string, (string Label, string Proxy)>
{
    ["own"] = ("own proxy (rotating)", Environment.GetEnvironmentVariable("PROXY_URL")!),
    ["tier1"] = ("cdpfleet-resi Tier 1, sticky US", $"cdpfleet-resi-country-us-session-c68{run}-lifetime-30"),
};
var setups = new List<(string Key, string Name, string Engine, IBrowserType Type, bool Headless)>
{
    ("chromium", "Chromium, headless", "chromium", playwright.Chromium, true),
    ("chrome", "Google Chrome, headful", "chrome", playwright.Chromium, false),
    ("edge", "Microsoft Edge, headful", "edge", playwright.Chromium, false),
    ("brave", "Brave, headful", "brave", playwright.Chromium, false),
    ("camoufox", "Camoufox, defaults", "camoufox", playwright.Firefox, true),
    ("firefox", "Firefox, headful", "firefox", playwright.Firefox, false),
    ("webkit", "WebKit, headful", "webkit", playwright.Webkit, false),
};
// Vendors' own public demo pages. Google's v3 demo asks for a score as soon as it loads and prints its
// server's verify response (with the score) on the page; hCaptcha's demo shows a checkbox widget.
const string Recaptcha = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php";
const string Hcaptcha = "https://accounts.hcaptcha.com/demo";

// Read what the page shows; the same expressions run in every language.
const string Score = """(() => { const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()""";
const string HcWidget = """(() => { const f = document.querySelector('iframe[src*="hcaptcha.com"]'); return f ? (f.getBoundingClientRect().height > 0) : false; })()""";

async Task<IBrowser> Launch((string Key, string Name, string Engine, IBrowserType Type, bool Headless) s, string network)
{
    var proxy = networks[network].Proxy;
    var body = new JsonObject { ["proxy"] = proxy, ["headless"] = s.Headless, ["record"] = false };
    // WebKit takes an authenticated HTTP proxy through the updatable route (see the WebKit page).
    if (s.Engine == "webkit" && Regex.IsMatch(proxy, "^https?:")) body["proxy_updatable"] = true;
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

async Task<bool> Open(IPage tab, string url)
{
    try { await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 }); return true; }
    catch (PlaywrightException) { return false; }
}

// One load; then wait up to 20 s for the score the demo prints. Nothing on the page is touched.
async Task<JsonObject> RecaptchaCell(IBrowserContext context)
{
    var tab = await context.NewPageAsync();
    if (!await Open(tab, Recaptcha)) { await tab.CloseAsync(); return new JsonObject { ["outcome"] = "error", ["score"] = null }; }
    double? score = null;
    for (var i = 0; i < 20 && score == null; i++)
    {
        await Task.Delay(1000);
        try
        {
            var v = await tab.EvaluateAsync<JsonElement>(Score);
            if (v.ValueKind == JsonValueKind.Number) score = v.GetDouble();
        }
        catch (PlaywrightException) { }
    }
    try { await tab.CloseAsync(); } catch (PlaywrightException) { }
    return new JsonObject { ["outcome"] = score == null ? "no score" : "score", ["score"] = score };
}

// hCaptcha only decides when someone clicks its checkbox, so a load shows whether the widget renders, nothing more.
async Task<JsonObject> HcaptchaCell(IBrowserContext context)
{
    var tab = await context.NewPageAsync();
    if (!await Open(tab, Hcaptcha)) { await tab.CloseAsync(); return new JsonObject { ["outcome"] = "error" }; }
    var shown = false;
    for (var i = 0; i < 12 && !shown; i++)
    {
        await Task.Delay(1000);
        try { shown = await tab.EvaluateAsync<bool>(HcWidget); } catch (PlaywrightException) { }
    }
    try { await tab.CloseAsync(); } catch (PlaywrightException) { }
    return new JsonObject { ["outcome"] = shown ? "widget shown" : "no widget" };
}

async Task<JsonObject> Row((string Key, string Name, string Engine, IBrowserType Type, bool Headless) s, string network)
{
    var output = new JsonObject { ["setup"] = s.Name, ["network"] = networks[network].Label };
    IBrowser browser;
    try { browser = await Launch(s, network); }
    catch (Exception)
    {
        output["recaptcha"] = new JsonObject { ["outcome"] = "error", ["score"] = null };
        output["hcaptcha"] = new JsonObject { ["outcome"] = "error" };
        return output;
    }
    // A session the server ends (e.g. its memory limit) leaves the remaining cells as errors.
    try
    {
        var context = await browser.NewContextAsync(); // a fresh context: every load is a first visit
        output["recaptcha"] = await RecaptchaCell(context);
        await context.CloseAsync();
    }
    catch (PlaywrightException) { output["recaptcha"] = new JsonObject { ["outcome"] = "error", ["score"] = null }; }
    try
    {
        var context = await browser.NewContextAsync();
        output["hcaptcha"] = await HcaptchaCell(context);
        await context.CloseAsync();
    }
    catch (PlaywrightException) { output["hcaptcha"] = new JsonObject { ["outcome"] = "error" }; }
    try { await browser.CloseAsync(); } catch (PlaywrightException) { }
    return output;
}

var rows = new JsonArray();
foreach (var network in new[] { "own", "tier1" })
    foreach (var s in setups) rows.Add(await Row(s, network));

var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["rows"] = rows };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
