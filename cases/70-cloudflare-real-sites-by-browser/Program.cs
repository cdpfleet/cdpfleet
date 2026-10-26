// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, LAB_SITES (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
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
var sites = Environment.GetEnvironmentVariable("LAB_SITES")!.Split(',').Select((u, i) => (Site: ((char)('A' + i)).ToString(), Url: u)).ToArray();

// Every browser the fleet offers that a Playwright 1.60 client can drive (Rebrowser needs 1.52, so it's left out).
var setups = new (string Id, string Name, string Engine, IBrowserType Type, JsonNode Headless)[]
{
    ("chrome_headful", "Chrome, headful", "chrome", playwright.Chromium, false),
    ("chrome_headless", "Chrome, headless", "chrome", playwright.Chromium, "new"),
    ("edge", "Edge, headful", "edge", playwright.Chromium, false),
    ("brave", "Brave, headful", "brave", playwright.Chromium, false),
    ("opera", "Opera, headful", "opera", playwright.Chromium, false),
    ("yandex", "Yandex, headful", "yandex", playwright.Chromium, false),
    ("whale", "Whale, headful", "whale", playwright.Chromium, false),
    ("chromium_headless", "Chromium, headless", "chromium", playwright.Chromium, true),
    ("patchright", "Patchright, headful", "patchright", playwright.Chromium, false),
    ("firefox", "Firefox, headful", "firefox", playwright.Firefox, false),
    ("webkit", "WebKit, headful", "webkit", playwright.Webkit, false),
    ("camoufox", "Camoufox, headful", "camoufox", playwright.Firefox, false),
};

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words.
const string State = """
(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 600)).replace(/\s+/g, ' ');
  const len = document.body ? document.body.innerText.length : 0;
  if (/sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) && len < 4000) return 'blocked';
  if (/just a moment|performing security verification|verify you are human|checking your browser|checking if the site connection is secure/i.test(t) && len < 4000) return 'challenge';
  return 'content';
})()
""";

async Task<string> Launch(string engine, JsonNode headless, string proxy, string? profile)
{
    var body = new JsonObject { ["proxy"] = proxy, ["headless"] = headless.DeepClone(), ["record"] = false };
    if (profile != null) body["profile"] = profile;
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session", JsonContent.Create(body));
        if (res.IsSuccessStatusCode) return (await res.Content.ReadFromJsonAsync<JsonObject>())!["wsUrl"]!.GetValue<string>();
        if (((int)res.StatusCode != 429 && (int)res.StatusCode != 503) || attempt == 5)
            throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5) * 1000);
    }
}

JsonObject Cell(string outcome, int status) => new() { ["outcome"] = outcome, ["status"] = status };

// One session, one load: the status of the page's own response, then its state 8 s later.
async Task<JsonObject> Visit((string Id, string Name, string Engine, IBrowserType Type, JsonNode Headless) s, string url, string proxy, string? profile = null)
{
    try
    {
        var browser = await s.Type.ConnectAsync(await Launch(s.Engine, s.Headless, proxy, profile),
            new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
        try
        {
            var context = profile != null ? browser.Contexts[0] : await browser.NewContextAsync();
            var tab = await context.NewPageAsync();
            int status;
            try
            {
                var res = await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 });
                status = res?.Status ?? 0;
            }
            catch (PlaywrightException) { return Cell("error", 0); }
            await tab.WaitForTimeoutAsync(8000);
            string outcome;
            try { outcome = await tab.EvaluateAsync<string>(State); } catch (PlaywrightException) { outcome = "error"; }
            if (outcome == "content" && status >= 400) outcome = "blocked";
            return Cell(outcome, status);
        }
        finally { await browser.CloseAsync(); }
    }
    catch (Exception) { return Cell("error", 0); }
}

// One sticky residential exit per site; its setups run one after another (one session at a time per
// site), and the sites run side by side. A network error gets one more try.
async Task<JsonObject> Site(string label, string url)
{
    var proxy = $"cdpfleet-resi-country-us-session-c70{run}{label}-lifetime-30";
    var results = new JsonObject();
    foreach (var s in setups)
    {
        var r = await Visit(s, url, proxy);
        if (r["outcome"]!.GetValue<string>() == "error") r = await Visit(s, url, proxy);
        results[s.Id] = r;
    }
    // A returning visitor: Camoufox with a profile, a first visit and then a second session.
    var name = $"case70-{run}-{label}";
    var camoufox = setups[^1];
    await Visit(camoufox, url, proxy, name);
    for (var i = 0; i < 60; i++)
    {
        var list = await http.GetFromJsonAsync<JsonObject>("https://cdpfleet.com/v1/me/profiles");
        var p = list?["profiles"]?.AsArray().FirstOrDefault(x => x?["name"]?.GetValue<string>() == name);
        if (p?["in_use"]?.GetValue<bool>() == false) break;
        await Task.Delay(2000);
    }
    results["camoufox_returning"] = await Visit(camoufox, url, proxy, name);
    await http.DeleteAsync($"https://cdpfleet.com/v1/me/profiles/{name}");
    return new JsonObject { ["site"] = label, ["results"] = results };
}

var done = await Task.WhenAll(sites.Select(x => Site(x.Site, x.Url)));
var names = new JsonObject();
foreach (var s in setups) names[s.Id] = s.Name;
names["camoufox_returning"] = "Camoufox, returning profile (2nd visit)";
var output = new JsonObject
{
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
    ["setups"] = names,
    ["sites"] = new JsonArray(done.Select(d => (JsonNode)d).ToArray()),
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
