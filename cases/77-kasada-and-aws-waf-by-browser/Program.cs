// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, LAB_SITES_KASADA_AWSWAF (comma-separated URLs of the pages to test; one load per page per setup, nothing clicked)
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
var sites = Environment.GetEnvironmentVariable("LAB_SITES_KASADA_AWSWAF")!.Split(',').Select((u, i) => (Site: ((char)('A' + i)).ToString(), Url: u)).ToArray();

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

// What the page shows after it loaded and had 8 s to run its checks, in the lab's words. AWS WAF's
// CAPTCHA ("Let's confirm you are human") is interactive: a challenge. A near-empty page 8 s later
// (Kasada's 429 answer is a blank page with a script) or an "access denied" page is blocked.
const string State = """
(() => {
  const text = document.body ? document.body.innerText : '';
  const t = (document.title + ' ' + text.slice(0, 600)).replace(/\s+/g, ' ');
  if (document.querySelector('#captcha-container, awswaf-captcha') || (/confirm you are human|verify you are (a )?human|are you a robot|solve the puzzle/i.test(t) && text.length < 4000)) return 'challenge';
  if (/access (to this page has been )?denied|has been blocked|you don.t have permission to access|request (unsuccessful|blocked)|too many requests/i.test(t) && text.length < 4000) return 'blocked';
  if (text.trim().length < 200) return 'blank';
  return 'content';
})()
""";

var kasadaUrl = new Regex(@"/ips\.js(\?|$)|/(tl|fp)(\?|$)");
var awsUrl = new Regex(@"awswaf\.com|/challenge\.js(\?|$)");
var vendorCookie = new Regex(@"^(KP_|aws-waf)", RegexOptions.IgnoreCase);

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

JsonObject Failed(string outcome) =>
    new() { ["outcome"] = outcome, ["status"] = 0, ["final_status"] = 0, ["kasada"] = false, ["aws_waf"] = false, ["cookies"] = new JsonArray() };

// One session, one load. Records the status of the page's first response and of the last main-frame
// document 8 s later (an interstitial that clears itself reloads the page), which vendor's scripts,
// headers or cookies showed up (cookie and header NAMES only; values are never read), and the state.
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
            var gate = new object();
            bool kasada = false, aws = false;
            string? wafAction = null;
            var docs = new List<(int Status, double At)>();
            var t0 = DateTime.UtcNow;
            var main = tab.MainFrame;
            tab.Request += (_, r) =>
            {
                lock (gate)
                {
                    if (kasadaUrl.IsMatch(r.Url) || r.Headers.Keys.Any(h => h.StartsWith("x-kpsdk", StringComparison.OrdinalIgnoreCase))) kasada = true;
                    if (awsUrl.IsMatch(r.Url)) aws = true;
                }
            };
            tab.Response += (_, r) =>
            {
                lock (gate)
                {
                    if (r.Headers.Keys.Any(h => h.StartsWith("x-kpsdk", StringComparison.OrdinalIgnoreCase))) kasada = true;
                    if (r.Headers.TryGetValue("x-amzn-waf-action", out var a) && !string.IsNullOrEmpty(a))
                    {
                        aws = true;
                        if ((a == "challenge" || a == "captcha") && wafAction == null) wafAction = a;
                    }
                    if (r.Request.IsNavigationRequest && r.Frame == main) docs.Add((r.Status, (DateTime.UtcNow - t0).TotalSeconds));
                }
            };
            int status;
            try
            {
                var res = await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 });
                status = res?.Status ?? 0;
            }
            catch (PlaywrightException e)
            {
                // A reset HTTP/2 connection is a refusal, not a network failure.
                return Failed(e.Message.Contains("ERR_HTTP2_PROTOCOL_ERROR") ? "blocked" : "error");
            }
            await tab.WaitForTimeoutAsync(8000);
            string outcome;
            try { outcome = await tab.EvaluateAsync<string>(State); } catch (PlaywrightException) { outcome = "error"; }
            var cookies = (await context.CookiesAsync()).Select(c => c.Name).Where(n => vendorCookie.IsMatch(n)).Distinct().OrderBy(n => n, StringComparer.Ordinal).ToList();
            lock (gate)
            {
                var (finalStatus, finalAt) = docs.Count > 0 ? docs[^1] : (status, 0.0);
                double? clearedS = null;
                // A 202/429 check that passed by itself reloads the page with a success status: cleared, even if
                // the new page hasn't rendered yet at the 8 s mark. Without that reload it never cleared.
                if (outcome == "content" || outcome == "blank")
                {
                    if (finalStatus >= 400) outcome = "blocked";
                    else if (status >= 400 || status == 202)
                    {
                        if (docs.Count > 1 && finalStatus != 202) { outcome = "cleared"; clearedS = Math.Round(finalAt, 1); } else outcome = "blocked";
                    }
                    else if (outcome == "blank") outcome = "blocked";
                }
                if (cookies.Any(n => n.StartsWith("KP_", StringComparison.OrdinalIgnoreCase))) kasada = true;
                if (cookies.Any(n => n.StartsWith("aws-waf", StringComparison.OrdinalIgnoreCase))) aws = true;
                var m = new JsonObject { ["outcome"] = outcome, ["status"] = status, ["final_status"] = finalStatus };
                if (clearedS != null) m["cleared_s"] = clearedS;
                m["kasada"] = kasada;
                m["aws_waf"] = aws;
                if (wafAction != null) m["waf_action"] = wafAction;
                m["cookies"] = new JsonArray(cookies.Select(n => (JsonNode)n!).ToArray());
                return m;
            }
        }
        finally { await browser.CloseAsync(); }
    }
    catch (Exception) { return Failed("error"); }
}

// One sticky residential exit per site; its setups run one after another (one session at a time per
// site), and the sites run side by side. A network error gets one more try.
async Task<JsonObject> Site(string label, string url)
{
    var proxy = $"cdpfleet-resi-country-us-session-c77{run}{label}-lifetime-30";
    var results = new JsonObject();
    foreach (var s in setups)
    {
        var r = await Visit(s, url, proxy);
        if (r["outcome"]!.GetValue<string>() == "error") r = await Visit(s, url, proxy);
        results[s.Id] = r;
    }
    // A returning visitor: Camoufox with a profile, a first visit and then a second session.
    var name = $"case77-{run}-{label}";
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
