// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one load per public test page per setup)
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL");
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

var setups = new (string Name, string Engine, bool Headless)[]
{
    ("Brave, headless", "brave", true),
    ("Brave, headful", "brave", false),
    ("Google Chrome, headful", "chrome", false),
};

// Two made-up sites on different registrable domains, served by page routing: the browser
// treats them as two first-party sites, and no request leaves the browser.
var sites = new Dictionary<string, string> { ["A"] = "https://site-a.example/", ["B"] = "https://site-b.test/" };
const string Html = "<!doctype html><title>lab 83</title><p>A page with nothing on it but this probe.</p>";

// What a fingerprinting script reads, the same in every language: a canvas hash (drawn twice),
// an OfflineAudioContext hash, cores, memory, the plugin list, the screen and the window size.
const string Probe = """
    (async () => {
      const fnv = (s) => { let h = 0x811c9dc5; for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 0x01000193) >>> 0; } return h.toString(16).padStart(8, '0'); };
      const draw = () => {
        const c = document.createElement('canvas'); c.width = 240; c.height = 60;
        const x = c.getContext('2d'); x.textBaseline = 'top'; x.font = '16px Arial';
        x.fillStyle = '#f60'; x.fillRect(100, 1, 62, 20);
        x.fillStyle = '#069'; x.fillText('cdpfleet fp 83 <@>', 2, 15);
        x.fillStyle = 'rgba(102, 204, 0, 0.7)'; x.fillText('cdpfleet fp 83 <@>', 4, 17);
        return c.toDataURL();
      };
      const audio = async () => {
        try {
          const ctx = new OfflineAudioContext(1, 44100, 44100);
          const o = ctx.createOscillator(); o.type = 'triangle'; o.frequency.value = 10000;
          const k = ctx.createDynamicsCompressor();
          k.threshold.value = -50; k.knee.value = 40; k.ratio.value = 12; k.attack.value = 0; k.release.value = 0.25;
          o.connect(k); k.connect(ctx.destination); o.start(0);
          const d = new Float32Array(500); (await ctx.startRendering()).copyFromChannel(d, 0, 4500);
          let s = ''; for (let i = 0; i < 500; i++) s += d[i].toFixed(10) + ',';
          return fnv(s);
        } catch (e) { return 'error: ' + e.name; }
      };
      const c1 = draw(), c2 = draw();
      const plugins = Array.from(navigator.plugins).map((p) => p.name);
      return {
        site: location.hostname,
        canvas: fnv(c1), canvas_same_in_page: c1 === c2,
        audio: await audio(),
        cores: navigator.hardwareConcurrency, memory: navigator.deviceMemory ?? null,
        plugins: fnv(plugins.join(',')), plugin_names: plugins,
        screen: screen.width + 'x' + screen.height, window: outerWidth + 'x' + outerHeight,
      };
    })()
    """;

// Each public test page's own verdict, read from its DOM; Ready is polled until the page has
// finished its checks. The same expressions run in every language.
var pages = new (string Key, string Url, string Ready, string Read)[]
{
    // A section CreepJS caught lying gets its hash marked with the "lies" class.
    ("creepjs", "https://abrahamjuliot.github.io/creepjs/",
        """/\d+% stealth/.test(document.body.innerText) && /privacy: \S/.test(document.body.innerText)""",
        """(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const m = /(\d+)% like headless[\s\S]*?(\d+)% headless:[\s\S]*?(\d+)% stealth/.exec(t); const privacy = /privacy: (\S+)/.exec(t); const noise = /rendering: (\d+% \w+ noise)/.exec(t); return { like_headless_pct: m ? +m[1] : null, headless_pct: m ? +m[2] : null, stealth_pct: m ? +m[3] : null, privacy: privacy ? privacy[1] : null, canvas_noise: noise ? noise[1] : null, lies: Array.from(document.querySelectorAll('.lies')).map((e) => (e.previousElementSibling ? e.previousElementSibling.innerText.trim() : '?')) }; })()"""),
    ("browserleaks", "https://browserleaks.com/canvas",
        """/Uniqueness\s+\d/.test(document.body.innerText)""",
        """(() => { const t = document.body.innerText; const sig = /Signature\s+([0-9A-Fa-f]{32})/.exec(t); const u = /Uniqueness\s+([^\n]+)/.exec(t); return { signature: sig ? sig[1] : null, uniqueness: u ? u[1].trim() : null }; })()"""),
    ("sannysoft", "https://bot.sannysoft.com/",
        """document.querySelectorAll('td.passed, td.failed').length > 10""",
        """(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()"""),
    ("deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
        """document.body.innerText.indexOf('"isBot"') >= 0""",
        """(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('"isBot"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()"""),
    // Only the bot verdict and the suspect score are read (the page also names proxy networks).
    ("fingerprint", "https://demo.fingerprint.com/playground",
        """/Suspect Score \d+/.test(document.body.innerText.replace(/\s+/g, ' '))""",
        """(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const bot = /\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\x20-\x7e]/g, '').replace(/\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()"""),
};

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<string> Launch(string engine, bool headless)
{
    var body = new JsonObject { ["proxy"] = proxy, ["record"] = false, ["headless"] = headless };
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{engine}/session", body);
        if (res.IsSuccessStatusCode) return (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
        var code = (int)res.StatusCode;
        if ((code != 429 && code != 503) || attempt == 5) throw new Exception($"launch {code} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5));
    }
}

async Task<IBrowserContext> SiteContext(IBrowser browser)
{
    var context = await browser.NewContextAsync();
    foreach (var url in sites.Values) await context.RouteAsync(url + "**", route => route.FulfillAsync(new() { ContentType = "text/html", Body = Html }));
    return context;
}

// One visit = a new tab in the given context, the probe, the tab closed.
async Task<JsonObject> Visit(IBrowserContext context, string label, int launchNo, int contextNo, string site)
{
    var page = await context.NewPageAsync();
    try
    {
        await page.GotoAsync(sites[site]);
        var v = new JsonObject { ["visit"] = label, ["launch"] = launchNo, ["context"] = contextNo };
        foreach (var (k, x) in JsonNode.Parse((await page.EvaluateAsync<JsonElement>(Probe)).GetRawText())!.AsObject()) v[k] = x?.DeepClone();
        return v;
    }
    finally
    {
        await page.CloseAsync();
    }
}

var auth = new BrowserTypeConnectOptions { Headers = new Dictionary<string, string> { ["x-api-key"] = key } };
var rows = new JsonArray();
foreach (var s in setups)
{
    var visits = new JsonArray();
    var verdicts = new JsonObject();
    // Launch 1: site A, site B, site A again in a new tab, then site A in a second context.
    var browser = await playwright.Chromium.ConnectAsync(await Launch(s.Engine, s.Headless), auth);
    try
    {
        var one = await SiteContext(browser);
        visits.Add(await Visit(one, "site A", 1, 1, "A"));
        visits.Add(await Visit(one, "site B", 1, 1, "B"));
        visits.Add(await Visit(one, "site A again", 1, 1, "A"));
        var two = await SiteContext(browser);
        visits.Add(await Visit(two, "site A, second context", 1, 2, "A"));
    }
    finally
    {
        await browser.CloseAsync();
    }
    // Launch 2: sites A and B again, then each public test page once, in a fresh context.
    browser = await playwright.Chromium.ConnectAsync(await Launch(s.Engine, s.Headless), auth);
    try
    {
        var three = await SiteContext(browser);
        visits.Add(await Visit(three, "site A, next launch", 2, 1, "A"));
        visits.Add(await Visit(three, "site B, next launch", 2, 1, "B"));
        var page = await (await browser.NewContextAsync()).NewPageAsync();
        foreach (var p in pages)
        {
            try
            {
                // A proxy tunnel drop is tried once more; a page that loaded is never reloaded.
                try { await page.GotoAsync(p.Url, new() { Timeout = 60000 }); }
                catch (PlaywrightException) { await page.GotoAsync(p.Url, new() { Timeout = 60000 }); }
                await page.WaitForFunctionAsync(p.Ready, null, new() { Timeout = 45000 });
                verdicts[p.Key] = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(p.Read)).GetRawText());
            }
            catch (Exception err)
            {
                var msg = err.Message.Split('\n')[0];
                verdicts[p.Key] = new JsonObject { ["error"] = msg.Length > 120 ? msg[..120] : msg };
            }
        }
    }
    finally
    {
        await browser.CloseAsync();
    }
    rows.Add(new JsonObject { ["setup"] = s.Name, ["engine"] = s.Engine, ["headless"] = s.Headless, ["visits"] = visits, ["pages"] = verdicts });
}
var output = new JsonObject { ["setups"] = rows };
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
