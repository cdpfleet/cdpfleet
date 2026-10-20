// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (an http(s) or socks5 proxy; one page load per test page per setup)
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

var setups = new (string Name, string Engine, IBrowserType Type, JsonObject Body)[]
{
    ("Chromium, headless", "chromium", playwright.Chromium, new JsonObject { ["headless"] = "new" }),
    ("Google Chrome, headful", "chrome", playwright.Chromium, new JsonObject { ["headless"] = false }),
    ("Patchright, headful", "patchright", playwright.Chromium, new JsonObject { ["headless"] = false }),
    ("Camoufox, defaults", "camoufox", playwright.Firefox, new JsonObject { ["headless"] = true }),
    ("WebKit, headless", "webkit", playwright.Webkit, new JsonObject { ["headless"] = true }),
};

// Each test page's own verdict, read from its DOM. The same expressions run in every language;
// Ready is polled until the page has finished its checks.
var pages = new (string Key, string Url, string Ready, string Read)[]
{
    ("sannysoft", "https://bot.sannysoft.com/",
        """document.querySelectorAll('td.passed, td.failed').length > 10""",
        """(() => { const rows = Array.from(document.querySelectorAll('tr')).filter((tr) => tr.querySelector('td.passed, td.failed, td.warn')); const failed = rows.filter((tr) => tr.querySelector('td.failed')).map((tr) => tr.cells[0].innerText.replace(/\s+/g, ' ').trim()); return { checks: rows.length, failed: failed.length, failed_names: failed }; })()"""),
    ("creepjs", "https://abrahamjuliot.github.io/creepjs/",
        """/\d+% stealth/.test(document.body.innerText)""",
        """(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const m = /(\d+)% like headless[\s\S]*?(\d+)% headless:[\s\S]*?(\d+)% stealth/.exec(t); return m ? { like_headless_pct: +m[1], headless_pct: +m[2], stealth_pct: +m[3] } : { like_headless_pct: null, headless_pct: null, stealth_pct: null }; })()"""),
    ("deviceandbrowserinfo", "https://deviceandbrowserinfo.com/are_you_a_bot",
        """document.body.innerText.indexOf('"isBot"') >= 0""",
        """(() => { const t = document.body.innerText; const s = t.indexOf('{', t.indexOf('"isBot"') - 10); let depth = 0, e = s; for (; e < t.length; e++) { if (t[e] === '{') depth++; if (t[e] === '}' && --depth === 0) break; } try { const j = JSON.parse(t.slice(s, e + 1)); return { is_bot: j.isBot, signals: Object.keys(j.details || {}).filter((k) => j.details[k] === true) }; } catch (err) { return { is_bot: null, signals: [] }; } })()"""),
    // Only the bot verdict and the suspect score are read (the page also names proxy networks).
    ("fingerprint", "https://demo.fingerprint.com/playground",
        """/Suspect Score \d+/.test(document.body.innerText.replace(/\s+/g, ' '))""",
        """(() => { const t = document.body.innerText.replace(/\s+/g, ' '); const bot = /\bBot (.+?) VPN /.exec(t); const score = /Suspect Score (\d+)/.exec(t); return { bot: bot ? bot[1].replace(/[^\x20-\x7e]/g, '').replace(/\s+/g, ' ').trim() : null, suspect_score: score ? +score[1] : null }; })()"""),
};

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<string> Launch(string engine, JsonObject options)
{
    var body = new JsonObject { ["proxy"] = proxy, ["record"] = false };
    foreach (var (k, v) in options) body[k] = v?.DeepClone();
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{engine}/session", body);
        if (res.IsSuccessStatusCode) return (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
        var code = (int)res.StatusCode;
        if ((code != 429 && code != 503) || attempt == 5) throw new Exception($"launch {code} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 5));
    }
}

var results = new JsonArray();
foreach (var s in setups)
{
    var wsUrl = await Launch(s.Engine, s.Body);
    var browser = await s.Type.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var row = new JsonObject { ["setup"] = s.Name, ["engine"] = s.Engine };
    try
    {
        var page = await (await browser.NewContextAsync()).NewPageAsync();
        foreach (var p in pages)
        {
            try
            {
                await page.GotoAsync(p.Url, new() { Timeout = 60000 });
                await page.WaitForFunctionAsync(p.Ready, null, new() { Timeout = 45000 });
                row[p.Key] = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(p.Read)).GetRawText());
            }
            catch (Exception err)
            {
                var msg = err.Message.Split('\n')[0];
                row[p.Key] = new JsonObject { ["error"] = msg.Length > 120 ? msg[..120] : msg };
            }
        }
    }
    finally
    {
        await browser.CloseAsync();
    }
    results.Add(row);
}
Console.WriteLine(results.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
