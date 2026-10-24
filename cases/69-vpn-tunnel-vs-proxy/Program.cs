// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (sticky US exits from the cdpfleet residential proxy, Tier 1; one load per bot-protection page)
using System.Diagnostics;
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

// One sticky exit per setup, used with and without the tunnel: the only difference between the two rows is vpn_tunnel.
string ExitFor(string id) => $"cdpfleet-resi-country-us-session-t{id}{run}-lifetime-30";
var setups = new (string Id, string Name, string Engine, IBrowserType Type, bool Headless)[]
{
    ("c", "Google Chrome, headful", "chrome", playwright.Chromium, false),
    ("f", "Camoufox, defaults", "camoufox", playwright.Firefox, true),
};
var networks = new (string Label, bool Tunnel)[] { ("residential proxy", false), ("residential proxy + vpn_tunnel", true) };

// The WebRTC leak test from case 54: ICE candidates against a public STUN server. A "srflx" candidate is the public IP
// the STUN server saw. The same expression runs in every language.
const string Gather = """
async () => {
  if (typeof RTCPeerConnection === 'undefined') return { api: false, candidates: [] };
  const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] });
  const candidates = [];
  pc.onicecandidate = (e) => { if (e.candidate && e.candidate.candidate) candidates.push(e.candidate.candidate); };
  pc.createDataChannel('probe');
  await pc.setLocalDescription(await pc.createOffer());
  await new Promise((done) => {
    const timer = setTimeout(done, 8000);
    pc.onicegatheringstatechange = () => { if (pc.iceGatheringState === 'complete') { clearTimeout(timer); done(); } };
  });
  pc.close();
  return { api: true, candidates };
}
""";
// The protocol of the page's own request: h2 over TCP, or h3 over QUIC (UDP).
const string Protocol = "performance.getEntriesByType('navigation')[0]?.nextHopProtocol || null";
// Cloudflare's interstitial or block page, or the real page.
const string CfState = """
(() => {
  const t = (document.title + ' ' + (document.body ? document.body.innerText : '').slice(0, 400)).replace(/\s+/g, ' ');
  return /sorry, you have been blocked|attention required|error 1020|access denied/i.test(t) ? 'blocked'
    : /just a moment|performing security verification|verify you are human|checking your browser/i.test(t) ? 'challenge' : 'page';
})()
""";
// DataDome's iframe from captcha-delivery.com: a device check, a CAPTCHA, or a block (t=bv) — as in case 65.
const string DdState = """
(() => {
  const f = [...document.querySelectorAll('iframe')].map((i) => i.src).find((s) => /captcha-delivery\.com/.test(s)) || '';
  const text = (document.body ? document.body.innerText : '').trim();
  return /[?&]t=bv(&|$)/.test(f) ? 'blocked' : /\/captcha\//.test(f) ? 'challenge' : f ? 'checking' : text.length > 200 ? 'page' : 'loading';
})()
""";
var ipv4 = new Regex(@"^\d+\.\d+\.\d+\.\d+$");
var nav = new PageGotoOptions { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 };

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<IBrowser> Launch((string Id, string Name, string Engine, IBrowserType Type, bool Headless) s, bool tunnel)
{
    var body = new JsonObject { ["proxy"] = ExitFor(s.Id), ["headless"] = s.Headless, ["record"] = false };
    if (tunnel) body["vpn_tunnel"] = true;
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{s.Engine}/session", body);
        if (res.IsSuccessStatusCode)
        {
            var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
            return await s.Type.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
        }
        var code = (int)res.StatusCode;
        if ((code != 429 && code != 503) || attempt == 6) throw new Exception($"launch {code} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5));
    }
}

// Residential peers drop a connection now and then: one retry.
async Task Go(IPage tab, string url)
{
    try { await tab.GotoAsync(url, nav); }
    catch (PlaywrightException) { await tab.GotoAsync(url, nav); }
}

async Task<string?> Str(IPage tab, string expr)
{
    try { return await tab.EvaluateAsync<string?>(expr); }
    catch (Exception) { return null; }
}

// One load, then watch the page once a second for 20 s (Cloudflare) or 12 s (DataDome) without touching it.
async Task<JsonObject> Watch(IBrowserContext context, string url, string kind)
{
    var tab = await context.NewPageAsync();
    var clock = Stopwatch.StartNew();
    try
    {
        try { await Go(tab, url); }
        catch (Exception) { return new JsonObject { ["outcome"] = "error", ["seconds"] = null }; }
        var seen = false;
        var until = Stopwatch.StartNew();
        var limit = kind == "cf" ? 20000 : 12000;
        while (until.ElapsedMilliseconds < limit)
        {
            var s = await Str(tab, kind == "cf" ? CfState : DdState);
            if (s == "blocked") return new JsonObject { ["outcome"] = "blocked", ["seconds"] = null };
            if (s == "challenge" && kind == "dd") return new JsonObject { ["outcome"] = "challenge", ["seconds"] = null };
            if (s == "challenge" || s == "checking") seen = true;
            if (s == "page")
                return seen ? new JsonObject { ["outcome"] = "cleared", ["seconds"] = (int)Math.Round(clock.Elapsed.TotalSeconds) } : new JsonObject { ["outcome"] = "content", ["seconds"] = null };
            await Task.Delay(1000);
        }
        return new JsonObject { ["outcome"] = seen ? "challenge" : "error", ["seconds"] = null };
    }
    finally { await tab.CloseAsync(); }
}

async Task<JsonObject> Row((string Id, string Name, string Engine, IBrowserType Type, bool Headless) s, (string Label, bool Tunnel) n)
{
    var browser = await Launch(s, n.Tunnel);
    var output = new JsonObject { ["setup"] = s.Name, ["network"] = n.Label };
    try
    {
        var context = await browser.NewContextAsync();
        var tab = await context.NewPageAsync();
        await Go(tab, "https://api.ipify.org/?format=json");
        output["exit_ip"] = JsonDocument.Parse(await tab.TextContentAsync("body") ?? "{}").RootElement.GetProperty("ip").GetString();
        var g = await tab.EvaluateAsync<JsonElement>($"({Gather})()");
        var ips = new List<string>();
        var sawMdns = false;
        foreach (var c in g.GetProperty("candidates").EnumerateArray())
        {
            // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
            var f = c.GetString()!.Split(' ');
            if (f.Length < 8) continue;
            if (f[4].EndsWith(".local")) sawMdns = true;
            if (ipv4.IsMatch(f[4]) && f[4] != "0.0.0.0" && !ips.Contains(f[4])) ips.Add(f[4]);
        }
        output["webrtc_api"] = g.GetProperty("api").GetBoolean();
        output["webrtc_ips"] = new JsonArray(ips.Select(ip => (JsonNode?)JsonValue.Create(ip)).ToArray());
        output["mdns_only"] = ips.Count == 0 && sawMdns;
        await tab.CloseAsync();
        // HTTP/3: a page announces it over HTTP/2 (Alt-Svc); the browser can switch to QUIC on a later request. Four loads
        // in fresh tabs, a pause between them.
        var protocols = new JsonArray();
        for (var i = 0; i < 4; i++)
        {
            var t = await context.NewPageAsync();
            try { await Go(t, "https://cloudflare-quic.com/"); } catch (Exception) { }
            protocols.Add((JsonNode?)JsonValue.Create(await Str(t, Protocol)));
            await t.CloseAsync();
            await Task.Delay(2500);
        }
        output["protocols"] = protocols;
        await context.CloseAsync();
        // The bot-protection pages, each in a fresh context: Cloudflare's challenge test page (case 64) and DataDome's
        // own site (case 65).
        foreach (var (k, url, kind) in new[] { ("scrapingcourse", "https://www.scrapingcourse.com/cloudflare-challenge", "cf"), ("datadome", "https://datadome.co/", "dd") })
        {
            var c = await browser.NewContextAsync();
            output[k] = await Watch(c, url, kind);
            await c.CloseAsync();
        }
    }
    finally { await browser.CloseAsync(); }
    return output;
}

var rows = new JsonArray();
foreach (var s in setups)
    foreach (var n in networks)
        rows.Add(await Row(s, n));
var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["rows"] = rows };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
