// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (each setup gets its own sticky US exit from the cdpfleet residential proxy; a few KB of traffic)
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
// A sticky residential exit per setup: one IP for the whole session, to compare with.
var run = Guid.NewGuid().ToString("N")[..6];
string StickyExit(int i) => $"cdpfleet-resi-country-us-session-w54{run}{i}-lifetime-10";

// What a WebRTC leak test runs: gather ICE candidates against a public STUN server. A "srflx"
// candidate carries the public IP the STUN server saw; UDP doesn't go through an HTTP/SOCKS proxy —
// on cdpfleet, proxied sessions can't send UDP at all, so only an anonymous mDNS host candidate is left.
const string Gather = @"async () => {
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
}";

var setups = new (string Name, string Engine, JsonObject Body)[]
{
    ("Chromium", "chromium", new JsonObject { ["headless"] = "new" }),
    ("Google Chrome", "chrome", new JsonObject { ["headless"] = "new" }),
    ("Firefox", "firefox", new JsonObject { ["headless"] = true }),
    ("Camoufox (defaults: geoip on)", "camoufox", new JsonObject { ["headless"] = true }),
    ("Camoufox, block_webrtc: true", "camoufox", new JsonObject { ["headless"] = true, ["block_webrtc"] = true }),
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// Residential peers drop a connection now and then: up to two retries.
async Task Go(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(url, new() { Timeout = 60000 }); return; }
        catch (PlaywrightException) when (attempt < 3) { await Task.Delay(2000); }
    }
}

var results = new JsonArray();
for (var i = 0; i < setups.Length; i++)
{
    var s = setups[i];
    var body = (JsonObject)s.Body.DeepClone();
    body["proxy"] = StickyExit(i);
    body["record"] = false;
    var session = await Launch(s.Engine, body);
    var type = s.Engine is "firefox" or "camoufox" ? playwright.Firefox : playwright.Chromium;
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var context = await browser.NewContextAsync();
        var page = await context.NewPageAsync();
        await Go(page, "https://api.ipify.org/?format=json");
        var exitIp = JsonDocument.Parse(await page.TextContentAsync("body") ?? "{}").RootElement.GetProperty("ip").GetString()!;
        var gathered = await page.EvaluateAsync<JsonElement>(Gather);
        var candidates = gathered.GetProperty("candidates").EnumerateArray().Select((c) => c.GetString()!).ToList();
        // candidate:<foundation> <component> <transport> <priority> <address> <port> typ <type> …
        var parsed = candidates.Select((c) => c.Split(' ')).Select((f) => (Address: f[4], Type: f[7])).ToList();
        var publicIps = parsed.Where((c) => Regex.IsMatch(c.Address, @"^\d+\.\d+\.\d+\.\d+$") && c.Address != "0.0.0.0").Select((c) => c.Address).Distinct().ToList();
        results.Add(new JsonObject
        {
            ["setup"] = s.Name,
            ["exit_ip"] = exitIp,
            ["webrtc_api"] = gathered.GetProperty("api").GetBoolean(),
            ["candidates"] = candidates.Count,
            ["mdns_host_candidates"] = parsed.Count((c) => c.Type == "host" && c.Address.EndsWith(".local")),
            ["public_ips"] = new JsonArray(publicIps.Select((ip) => (JsonNode)JsonValue.Create(ip)!).ToArray()),
            ["shows_exit_ip"] = publicIps.Contains(exitIp),
            ["leaks_other_ip"] = publicIps.Any((ip) => ip != exitIp),
        });
    }
    finally
    {
        await browser.CloseAsync();
    }
}
Console.WriteLine(results.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
