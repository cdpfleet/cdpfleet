// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (residential exits come from cdpfleet-resi tokens)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var run = Convert.ToString(DateTimeOffset.UtcNow.ToUnixTimeMilliseconds(), 16)[^4..];
var countries = new (string Code, string Locale)[] { ("US", "en-US"), ("DE", "de-DE"), ("JP", "ja-JP") };
// (label, engine, launch options, match = context locale + timezone from a first lookup of the exit)
var setups = new (string Label, string Engine, Dictionary<string, object> Options, bool Match)[]
{
    ("Camoufox (geoip on, default)", "camoufox", new() { ["geoip"] = true }, false),
    ("Camoufox, geoip: false", "camoufox", new() { ["geoip"] = false }, false),
    ("Camoufox, locale: \"pt-BR\"", "camoufox", new() { ["geoip"] = true, ["locale"] = "pt-BR" }, false),
    ("Chrome headful, no locale", "chrome", new() { ["headless"] = false }, false),
    ("Chrome headful, context locale + timezoneId from the exit", "chrome", new() { ["headless"] = false }, true),
};

// What a site can read and compare, run on the exit-lookup page itself (ip-api.com's JSON, so the
// page knows the exit): exit country and timezone, languages, Accept-Language (echoed by httpbin),
// Intl's timezone and locale, a formatted number, and the WebRTC public address (compared with the
// exit, never returned).
const string Check = """
(async () => {
  const exit = JSON.parse(document.body.innerText);
  const headers = await fetch('https://httpbin.org/headers').then((r) => r.json()).catch(() => ({ headers: {} }));
  const srflx = await new Promise((done) => {
    const ips = new Set();
    try {
      const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
      pc.createDataChannel('x');
      pc.onicecandidate = (e) => { const c = e.candidate && e.candidate.candidate; if (c && c.includes(' srflx ')) ips.add(c.split(' ')[4]); };
      pc.createOffer().then((o) => pc.setLocalDescription(o));
      setTimeout(() => { pc.close(); done([...ips]); }, 4000);
    } catch { done([]); }
  });
  const intl = Intl.DateTimeFormat().resolvedOptions();
  const acceptLanguage = (headers.headers['Accept-Language'] || '').split(',')[0];
  const out = {
    exit_country: exit.countryCode, exit_timezone: exit.timezone,
    languages: navigator.languages.join(', '), accept_language: acceptLanguage,
    timezone: intl.timeZone, intl_locale: intl.locale, number: (1234567.891).toLocaleString(),
    webrtc: srflx.length === 0 ? 'none' : srflx.every((ip) => ip === exit.query) ? 'exit IP' : 'another IP',
  };
  out.mismatches = [
    out.timezone !== exit.timezone && 'timezone ≠ IP timezone',
    (navigator.language.split('-')[1] || '').toUpperCase() !== exit.countryCode && 'language region ≠ IP country',
    acceptLanguage.toLowerCase() !== navigator.language.toLowerCase() && 'Accept-Language ≠ navigator.language',
    !intl.locale.toLowerCase().startsWith(navigator.language.slice(0, 2).toLowerCase()) && 'Intl locale ≠ navigator.language',
    out.webrtc === 'another IP' && 'WebRTC shows another IP',
  ].filter(Boolean);
  return out;
})()
""";

using var http = new HttpClient { Timeout = TimeSpan.FromMinutes(3) };
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> Measure((string Code, string Locale) c, (string Label, string Engine, Dictionary<string, object> Options, bool Match) s)
{
    var cc = c.Code.ToLowerInvariant();
    var row = new JsonObject { ["setup"] = s.Label, ["country"] = c.Code };
    var camoufox = s.Engine == "camoufox";
    var body = new Dictionary<string, object> { ["proxy"] = $"cdpfleet-resi-country-{cc}-session-c74{cc}{run}-lifetime-10", ["headless"] = camoufox, ["record"] = false };
    if (camoufox) body["main_world_eval"] = true;
    foreach (var (k, v) in s.Options) body[k] = v;
    var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{s.Engine}/session", body);
    if (!res.IsSuccessStatusCode) { row["error"] = $"launch {(int)res.StatusCode}"; return row; }
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await (camoufox ? playwright.Firefox : playwright.Chromium).ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var options = new BrowserNewContextOptions();
        if (s.Match) // look the exit up first, then set the context to match it
        {
            var probe = await browser.NewContextAsync();
            var pg = await probe.NewPageAsync();
            await pg.GotoAsync("http://ip-api.com/json/?fields=timezone", new() { Timeout = 60000 });
            options.Locale = c.Locale;
            options.TimezoneId = JsonNode.Parse((await pg.TextContentAsync("body"))!)!["timezone"]!.GetValue<string>();
            await probe.CloseAsync();
        }
        var page = await (await browser.NewContextAsync(options)).NewPageAsync();
        await page.GotoAsync("http://ip-api.com/json/?fields=countryCode,timezone,query", new() { Timeout = 60000 });
        var seen = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(camoufox ? "mw:" + Check : Check)).GetRawText())!.AsObject();
        foreach (var (k, v) in seen.ToList()) { seen.Remove(k); row[k] = v; }
    }
    catch (Exception err) { row["error"] = err.Message.Split('\n')[0]; }
    finally { await browser.CloseAsync(); }
    return row;
}

// Countries in parallel, setups one after another within a country (one exit per country).
var perCountry = await Task.WhenAll(countries.Select(async c =>
{
    var rows = new List<JsonObject>();
    foreach (var s in setups) rows.Add(await Measure(c, s));
    return rows;
}));
Console.WriteLine(JsonSerializer.Serialize(perCountry.SelectMany(r => r), new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
