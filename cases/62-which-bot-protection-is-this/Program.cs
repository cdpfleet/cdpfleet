// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

// Public pages, each loaded once: (url, expected vendor, demo name or null). Demo pages are the
// protection vendors' own test pages and are reported by name; for the rest only the counts per
// vendor are reported. The expected vendor is what a first survey (2026-10-19) found.
var pages = new (string Url, string Expect, string? Demo)[]
{
    ("https://nowsecure.nl/", "turnstile", "nowsecure.nl (Turnstile test page)"),
    ("https://www.scrapingcourse.com/cloudflare-challenge", "cloudflare", "scrapingcourse.com Cloudflare challenge"),
    ("https://peet.ws/turnstile-test/non-interactive.html", "turnstile", "peet.ws Turnstile test (non-interactive)"),
    ("https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php", "recaptcha", "Google reCAPTCHA v3 demo"),
    ("https://accounts.hcaptcha.com/demo", "hcaptcha", "hCaptcha demo"),
    ("https://www.crunchbase.com/", "cloudflare", null),
    ("https://discord.com/", "cloudflare", null),
    ("https://www.glassdoor.com/", "cloudflare", null),
    ("https://www.tripadvisor.com/", "datadome", null),
    ("https://www.etsy.com/", "datadome", null),
    ("https://soundcloud.com/", "datadome", null),
    ("https://www.homedepot.com/", "akamai", null),
    ("https://www.usps.com/", "akamai", null),
    ("https://www.delta.com/", "akamai", null),
    ("https://www.zillow.com/", "human", null),
    ("https://www.walmart.com/", "human", null),
    ("https://www.realestate.com.au/", "kasada", null),
    ("https://www.ticketmaster.com/", "kasada", null),
    ("https://www.amazon.com/", "awswaf", null),
    ("https://www.booking.com/", "awswaf", null),
};
string[] vendorOrder = { "cloudflare", "turnstile", "datadome", "akamai", "human", "kasada", "imperva", "awswaf", "recaptcha", "hcaptcha" };

// A lenient host match: new Uri() rejects the unencoded characters some pages put in URLs.
string Host(string u) { var m = Regex.Match(u, "^[a-z]+://([^/:?#]+)"); return m.Success ? m.Groups[1].Value.ToLowerInvariant() : ""; }
bool AnyHost(List<string> urls, string re) => urls.Any(u => Regex.IsMatch(Host(u), re));

// Each vendor leaves response headers, cookies or requests to its own hosts.
var signals = new (string Vendor, string Label, Func<Dictionary<string, string>, List<string>, List<string>, bool> Test)[]
{
    ("cloudflare", "cf-ray header", (h, c, u) => h.ContainsKey("cf-ray")),
    ("cloudflare", "__cf_bm / cf_clearance cookie", (h, c, u) => c.Any(x => x is "__cf_bm" or "cf_clearance" or "__cflb")),
    ("cloudflare", "/cdn-cgi/challenge-platform request", (h, c, u) => u.Any(x => x.Contains("/cdn-cgi/challenge-platform"))),
    ("turnstile", "Turnstile widget", (h, c, u) => u.Any(x => x.Contains("challenges.cloudflare.com") && x.Contains("turnstile"))),
    ("datadome", "x-datadome header", (h, c, u) => h.ContainsKey("x-datadome") || h.ContainsKey("x-dd-b")),
    ("datadome", "datadome cookie", (h, c, u) => c.Contains("datadome")),
    ("datadome", "DataDome host (captcha-delivery.com, datadome.co)", (h, c, u) => AnyHost(u, @"captcha-delivery\.com|datadome\.co")),
    ("akamai", "_abck / bm_sz cookie", (h, c, u) => c.Any(x => x is "_abck" or "bm_sz" or "ak_bmsc" or "bm_sv")),
    ("akamai", "akamai header", (h, c, u) => h.Keys.Any(k => k.StartsWith("akamai") || k.StartsWith("x-akamai"))),
    ("human", "_px cookie", (h, c, u) => c.Any(x => x.StartsWith("_px"))),
    ("human", "HUMAN host (px-cdn.net, px-cloud.net)", (h, c, u) => AnyHost(u, @"px-cdn\.net|px-cloud\.net|perimeterx\.net|pxchk\.net")),
    ("kasada", "x-kpsdk header", (h, c, u) => h.Keys.Any(k => k.StartsWith("x-kpsdk"))),
    ("kasada", "ips.js / kpsdk request", (h, c, u) => u.Any(x => x.Contains("/ips.js") || x.Contains("kpsdk"))),
    ("imperva", "x-iinfo header", (h, c, u) => h.ContainsKey("x-iinfo")),
    ("imperva", "incap_ses / visid_incap cookie", (h, c, u) => c.Any(x => Regex.IsMatch(x, "^(incap_ses|visid_incap|nlbi_)"))),
    ("awswaf", "aws-waf-token cookie", (h, c, u) => c.Contains("aws-waf-token")),
    ("awswaf", "AWS WAF host (awswaf.com)", (h, c, u) => AnyHost(u, @"awswaf\.com")),
    ("recaptcha", "reCAPTCHA script", (h, c, u) => u.Any(x => x.Contains("google.com/recaptcha") || x.Contains("recaptcha.net"))),
    ("hcaptcha", "hCaptcha host", (h, c, u) => AnyHost(u, @"hcaptcha\.com")),
};
var challengeRe = new Regex("just a moment|checking your browser|verify you are human|press & hold|press and hold|are you a robot|human touch", RegexOptions.IgnoreCase);
var blockedRe = new Regex("access denied|access to this page has been denied|attention required|you have been blocked|request blocked|error 1020|pardon our interruption", RegexOptions.IgnoreCase);

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient { Timeout = TimeSpan.FromSeconds(120) };
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// One load in a fresh context: DOM ready + 6 s for the protection scripts to run, then the main
// response's headers, the cookies and every requested URL.
async Task<(string Outcome, List<(string Vendor, string Label)> Hits)> Survey(string url)
{
    var context = await browser.NewContextAsync();
    var tab = await context.NewPageAsync();
    var urls = new List<string>();
    tab.Request += (_, r) => { lock (urls) urls.Add(r.Url); };
    var h = new Dictionary<string, string>();
    string outcome;
    try
    {
        var r = await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 });
        var status = r?.Status ?? 0;
        if (r != null) h = await r.AllHeadersAsync();
        await tab.WaitForTimeoutAsync(6000);
        var title = await tab.TitleAsync();
        string text;
        try { text = Regex.Replace(await tab.Locator("body").InnerTextAsync(), @"\s+", " "); } catch (PlaywrightException) { text = ""; }
        var frames = string.Join(" ", tab.Frames.Select(f => f.Url));
        if (challengeRe.IsMatch($"{title} {text[..Math.Min(800, text.Length)]}") || frames.Contains("captcha-delivery.com")) outcome = "challenge";
        else if (blockedRe.IsMatch($"{title} {text[..Math.Min(400, text.Length)]}") || (status is 403 or 429 or 444 && text.Length < 1500)) outcome = "blocked";
        else outcome = "content";
    }
    catch (Exception e) when (e is PlaywrightException or TimeoutException)
    {
        outcome = "error";
    }
    var cookies = (await context.CookiesAsync()).Select(c => c.Name).ToList();
    await context.CloseAsync();
    List<string> snapshot;
    lock (urls) snapshot = urls.ToList();
    var hits = signals.Where(s => s.Test(h, cookies, snapshot)).Select(s => (s.Vendor, s.Label)).ToList();
    return (outcome, hits);
}

try
{
    var vendors = vendorOrder.ToDictionary(v => v, v => new JsonObject
    {
        ["vendor"] = v, ["pages"] = 0, ["signals"] = new JsonObject(),
        ["outcomes"] = new JsonObject { ["content"] = 0, ["challenge"] = 0, ["blocked"] = 0, ["error"] = 0 },
    });
    var demoPages = new JsonArray();
    int none = 0, expectedMatched = 0;
    foreach (var page in pages)
    {
        var (outcome, hits) = await Survey(page.Url);
        var found = hits.Select(x => x.Vendor).Distinct().ToList();
        if (found.Count == 0) none++;
        if (found.Contains(page.Expect)) expectedMatched++;
        foreach (var v in found)
        {
            vendors[v]["pages"] = (int)vendors[v]["pages"]! + 1;
            var oc = vendors[v]["outcomes"]!.AsObject();
            oc[outcome] = (int)oc[outcome]! + 1;
        }
        foreach (var (v, label) in hits)
        {
            var sig = vendors[v]["signals"]!.AsObject();
            sig[label] = (sig.ContainsKey(label) ? (int)sig[label]! : 0) + 1;
        }
        if (page.Demo != null)
            demoPages.Add(new JsonObject { ["page"] = page.Demo, ["vendors"] = new JsonArray(found.Select(v => (JsonNode)v!).ToArray()), ["outcome"] = outcome });
    }
    var output = new JsonObject
    {
        ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
        ["pages"] = pages.Length,
        ["no_vendor_found"] = none,
        ["expected_vendor_found"] = expectedMatched,
        ["vendors"] = new JsonArray(vendorOrder.Where(v => (int)vendors[v]["pages"]! > 0).Select(v => (JsonNode)vendors[v]).ToArray()),
        ["demo_pages"] = demoPages,
    };
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
