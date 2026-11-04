// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (one session at a time; public test pages only; nothing is solved or retried to get a result)
using System.Diagnostics;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
// One sticky residential exit for the whole run, shared by every setup: a difference between rows is the browser
// and how it was driven, not the IP.
var proxy = $"cdpfleet-resi-country-us-session-c81{Convert.ToHexString(RandomNumberGenerator.GetBytes(3)).ToLowerInvariant()}-lifetime-30";
const int Repeats = 3;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// Camoufox's humanize option: false = the cursor jumps straight to the target, true (the default) = a curved path of
// up to 1.5 s, a number = a path of up to that many seconds. Chrome is the reference: plain Playwright clicks.
var setups = new List<(string Key, string Name, string Engine, IBrowserType Type, JsonObject Body)>
{
    ("cf_off", "Camoufox, humanize: false", "camoufox", playwright.Firefox, new JsonObject { ["headless"] = true, ["humanize"] = false }),
    ("cf_on", "Camoufox, humanize: true (default)", "camoufox", playwright.Firefox, new JsonObject { ["headless"] = true, ["humanize"] = true }),
    ("cf_slow", "Camoufox, humanize: 3", "camoufox", playwright.Firefox, new JsonObject { ["headless"] = true, ["humanize"] = 3 }),
    ("chrome", "Google Chrome, headful", "chrome", playwright.Chromium, new JsonObject { ["headless"] = false }),
};
// The same actions in every cell; only the typing speed differs between the two form runs of a session.
var typing = new List<(string Name, float Delay)> { ("fast", 0), ("paced", 120) }; // ms between keys
const string Email = "lab81@example.com";
const string Password = "lab-81!pass"; // the demo form wants 8+ characters with a special character

// deviceandbrowserinfo.com's behavioural bot test: a demo login form that records clicks, keystrokes and timing
// and, on submit, posts them with a fingerprint to its own endpoint, which answers human or bot with its flags.
const string Form = "https://deviceandbrowserinfo.com/are_you_a_bot_interactions";
// Google's public reCAPTCHA v3 demo prints its server's verify response (with the score); its "Try again" link reloads it.
const string Recaptcha = "https://recaptcha-demo.appspot.com/recaptcha-v3-request-scores.php";

// The same expressions run in every language.
// Count mousemove events on the page, so the table shows what humanize changed.
const string CountMoves = "(() => { window.__lab81 = 0; document.addEventListener('mousemove', () => { window.__lab81++; }, true); return true; })()";
// The verdict the page shows after submit: isBot and every detail flag that is true.
const string Verdict = """
    (() => {
      const e = document.getElementById('jsonResult');
      const t = e ? e.innerText : '';
      if (t.length < 10) return null;
      const r = JSON.parse(t);
      const d = r.details || {};
      return { is_bot: r.isBot === true, flags: Object.keys(d).filter((k) => d[k] === true), moves: window.__lab81 };
    })()
    """;
// What the page itself measured about the interaction: the "interactions" part of the body it posted, which is
// embedded as a JSON string and passed to this function.
const string Measured = """
    (body) => {
      const i = JSON.parse(body).interactions || {};
      const s = i.emailTimingStats || {};
      return {
        time_to_fill_ms: Math.round(i.timeToFill),
        exact_center_clicks: [i.hasClickedEmailFieldExactCenter, i.hasClickedPasswordFieldExactCenter, i.hasClickedSubmitExactCenter].filter(Boolean).length,
        untrusted_event: i.hasUntrustedEvent === true,
        cdp_mouse_leak: i.cdpMouseLeak === true,
        key_gap_median_ms: typeof s.medianTimeBetweenKeys === 'number' ? Math.round(s.medianTimeBetweenKeys) : null,
      };
    }
    """;
const string Score = """(() => { if (window.__lab81rc) return null; const m = /"score"\s*:\s*([0-9.]+)/.exec(document.body ? document.body.innerText : ''); return m ? Number(m[1]) : null; })()""";

async Task<IBrowser> Launch((string Key, string Name, string Engine, IBrowserType Type, JsonObject Body) s)
{
    var body = new JsonObject { ["proxy"] = proxy, ["record"] = false };
    foreach (var (k, v) in s.Body) body[k] = v?.DeepClone();
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

// A load that fails on the network (proxy timeout) is tried once more; the page's answer is never retried.
async Task<bool> Open(IPage tab, string url)
{
    for (var attempt = 1; attempt <= 2; attempt++)
    {
        try { await tab.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 30000 }); return true; }
        catch (PlaywrightException) { /* network failure */ }
        catch (TimeoutException) { /* network failure */ }
    }
    return false;
}

async Task<JsonElement?> Eval(IPage tab, string expr)
{
    try
    {
        var v = await tab.EvaluateAsync<JsonElement>(expr);
        return v.ValueKind is JsonValueKind.Null or JsonValueKind.Undefined ? null : v;
    }
    catch (PlaywrightException) { return null; }
}

JsonObject EmptyRun(JsonObject @base, string name) => new()
{
    ["setup"] = @base["setup"]!.DeepClone(), ["key"] = @base["key"]!.DeepClone(), ["repeat"] = @base["repeat"]!.DeepClone(),
    ["typing"] = name, ["verdict"] = "error", ["flags"] = new JsonArray(), ["moves"] = null, ["action_seconds"] = null, ["measured"] = null,
};

// One form run: click the email field, type, click the password field, type, click Login; read the verdict.
async Task<JsonObject> FormRun(IBrowser browser, JsonObject @base, string name, float delay)
{
    var output = EmptyRun(@base, name);
    IBrowserContext? context = null;
    try
    {
        context = await browser.NewContextAsync(); // a fresh context: every run is a first visit
        var tab = await context.NewPageAsync();
        string? posted = null;
        tab.Request += (_, req) => { if (req.Url.Contains("/fingerprint_bot_test")) posted = req.PostData; };
        if (!await Open(tab, Form)) return output;
        await Task.Delay(1000);
        await tab.EvaluateAsync(CountMoves);
        var watch = Stopwatch.StartNew();
        await tab.ClickAsync("#email");
        await tab.Keyboard.TypeAsync(Email, new() { Delay = delay });
        await tab.ClickAsync("#password");
        await tab.Keyboard.TypeAsync(Password, new() { Delay = delay });
        await tab.ClickAsync("#loginForm button[type=submit]");
        output["action_seconds"] = Math.Round(watch.Elapsed.TotalSeconds * 10) / 10;
        JsonElement? v = null;
        for (var i = 0; i < 60 && v == null; i++) { await Task.Delay(500); v = await Eval(tab, Verdict); }
        if (v is not { } verdict) { output["verdict"] = "no verdict"; return output; }
        output["verdict"] = verdict.GetProperty("is_bot").GetBoolean() ? "bot" : "human";
        output["flags"] = JsonNode.Parse(verdict.GetProperty("flags").GetRawText());
        output["moves"] = verdict.GetProperty("moves").GetInt32();
        if (posted != null && await Eval(tab, $"({Measured})({JsonSerializer.Serialize(posted)})") is { } m)
        {
            var gap = m.GetProperty("key_gap_median_ms");
            output["measured"] = new JsonObject
            {
                ["time_to_fill_ms"] = m.GetProperty("time_to_fill_ms").GetInt32(),
                ["exact_center_clicks"] = m.GetProperty("exact_center_clicks").GetInt32(),
                ["untrusted_event"] = m.GetProperty("untrusted_event").GetBoolean(),
                ["cdp_mouse_leak"] = m.GetProperty("cdp_mouse_leak").GetBoolean(),
                ["key_gap_median_ms"] = gap.ValueKind == JsonValueKind.Number ? gap.GetInt32() : null,
            };
        }
        return output;
    }
    catch (Exception e) when (e is PlaywrightException or TimeoutException or KeyNotFoundException or InvalidOperationException)
    {
        return output;
    }
    finally
    {
        if (context != null) try { await context.CloseAsync(); } catch (PlaywrightException) { }
    }
}

async Task<double?> ReadScore(IPage tab) => await Eval(tab, Score) is { } v ? v.GetDouble() : null;

// Google's v3 demo: wait for the score it asks for on load, move the cursor and click "Try again", read the new score.
async Task<JsonObject> RecaptchaRun(IBrowser browser, JsonObject @base)
{
    double? before = null, after = null;
    IBrowserContext? context = null;
    try
    {
        context = await browser.NewContextAsync();
        var tab = await context.NewPageAsync();
        if (await Open(tab, Recaptcha))
        {
            for (var i = 0; i < 40 && before == null; i++) { await Task.Delay(500); before = await ReadScore(tab); }
            await tab.EvaluateAsync("window.__lab81rc = 1"); // marks this document, so the score is read from the reloaded one
            await tab.Mouse.MoveAsync(300, 300);
            await tab.Mouse.MoveAsync(600, 200);
            await tab.ClickAsync("a[href=\"/recaptcha-v3-request-scores.php\"]");
            for (var i = 0; i < 40 && after == null; i++) { await Task.Delay(500); after = await ReadScore(tab); }
        }
    }
    catch (Exception e) when (e is PlaywrightException or TimeoutException) { /* keep what was read */ }
    finally
    {
        if (context != null) try { await context.CloseAsync(); } catch (PlaywrightException) { }
    }
    var output = (JsonObject)@base.DeepClone();
    output["before"] = before;
    output["after"] = after;
    return output;
}

var runs = new JsonArray();
var scores = new JsonArray();
// Serial: one session at a time; the repeats are interleaved so a slow minute doesn't land on one setup.
for (var repeat = 1; repeat <= Repeats; repeat++)
{
    foreach (var s in setups)
    {
        var @base = new JsonObject { ["setup"] = s.Name, ["key"] = s.Key, ["repeat"] = repeat };
        IBrowser browser;
        try { browser = await Launch(s); }
        catch (Exception)
        {
            foreach (var t in typing) runs.Add(EmptyRun(@base, t.Name));
            var sc = (JsonObject)@base.DeepClone();
            sc["before"] = null;
            sc["after"] = null;
            scores.Add(sc);
            continue;
        }
        foreach (var t in typing) runs.Add(await FormRun(browser, @base, t.Name, t.Delay));
        scores.Add(await RecaptchaRun(browser, @base));
        try { await browser.CloseAsync(); } catch (PlaywrightException) { }
    }
}

var result = new JsonObject { ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["runs"] = runs, ["recaptcha"] = scores };
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
