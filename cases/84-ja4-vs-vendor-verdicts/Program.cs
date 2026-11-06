// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY
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

// The same twelve setups as the real-site lab cases 70-77 (Rebrowser needs a 1.52 client, so it's left out).
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

async Task<string> Launch(string engine, JsonNode headless, string proxy)
{
    var body = new JsonObject { ["proxy"] = proxy, ["headless"] = headless.DeepClone(), ["record"] = false };
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session", JsonContent.Create(body));
        if (res.IsSuccessStatusCode) return (await res.Content.ReadFromJsonAsync<JsonObject>())!["wsUrl"]!.GetValue<string>();
        if (((int)res.StatusCode != 429 && (int)res.StatusCode != 503) || attempt == 5)
            throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5) * 1000);
    }
}

// A string field (nested path) of the echo's answer, or null when missing or empty.
static string? Str(JsonNode? n, params string[] path)
{
    foreach (var p in path) n = n is JsonObject o ? o[p] : null;
    var s = n is JsonValue v && v.TryGetValue<string>(out var x) ? x : null;
    return string.IsNullOrEmpty(s) ? null : s;
}

static JsonObject Fp(string? httpVersion, string? ja4, string? ja4r, string? peet, string? h2, string? h2Hash, string? ua) => new()
{
    ["http_version"] = httpVersion, ["ja4"] = ja4, ["ja4_r"] = ja4r, ["peetprint_hash"] = peet,
    ["akamai_h2"] = h2, ["akamai_h2_hash"] = h2Hash, ["user_agent"] = ua,
};

// The browser itself loads a TLS echo page, which answers with the ClientHello and HTTP/2 frames
// it received from this very browser. tls.peet.ws first; tls.browserleaks.com/json only if peet
// doesn't answer (it has had outages). Each is one page load in a fresh tab.
var echoes = new (string Source, string Url, Func<JsonNode, JsonObject> Read)[]
{
    ("tls.peet.ws", "https://tls.peet.ws/api/all", j => Fp(Str(j, "http_version"), Str(j, "tls", "ja4"), Str(j, "tls", "ja4_r"),
        Str(j, "tls", "peetprint_hash"), Str(j, "http2", "akamai_fingerprint"), Str(j, "http2", "akamai_fingerprint_hash"), Str(j, "user_agent"))),
    ("tls.browserleaks.com", "https://tls.browserleaks.com/json", j => Fp(null, Str(j, "ja4"), Str(j, "ja4_r"),
        null, Str(j, "akamai_text"), Str(j, "akamai_hash"), Str(j, "user_agent"))),
};

async Task<JsonObject> Fingerprint(IBrowser browser)
{
    var context = await browser.NewContextAsync();
    string? last = null;
    foreach (var (source, url, read) in echoes)
    {
        var tab = await context.NewPageAsync();
        try
        {
            var res = await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 30000 });
            var fp = read(JsonNode.Parse(await res!.TextAsync())!);
            if (fp["ja4"] != null)
            {
                var m = new JsonObject { ["source"] = source };
                foreach (var (k, v) in fp) m[k] = v?.DeepClone();
                return m;
            }
            last = $"{source}: no ja4 in the answer";
        }
        catch (Exception e)
        {
            last = $"{source}: {e.Message.Split('\n')[0]}";
        }
        finally
        {
            try { await tab.CloseAsync(); } catch { }
        }
    }
    throw new Exception(last);
}

// One sticky residential exit for the whole run, and one session at a time.
var proxy = $"cdpfleet-resi-country-us-session-c84{run}-lifetime-30";
var fingerprints = new JsonObject();
foreach (var s in setups)
{
    try
    {
        var browser = await s.Type.ConnectAsync(await Launch(s.Engine, s.Headless, proxy),
            new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
        try
        {
            var m = new JsonObject { ["version"] = browser.Version };
            foreach (var (k, v) in await Fingerprint(browser)) m[k] = v?.DeepClone();
            fingerprints[s.Id] = m;
        }
        finally
        {
            await browser.CloseAsync();
        }
    }
    catch (Exception e)
    {
        var msg = e.Message.Split('\n')[0];
        fingerprints[s.Id] = new JsonObject { ["error"] = msg[..Math.Min(200, msg.Length)] };
    }
}

var output = new JsonObject
{
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
    ["setups"] = new JsonObject(setups.Select(s => KeyValuePair.Create(s.Id, (JsonNode?)s.Name))),
    ["fingerprints"] = fingerprints,
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
