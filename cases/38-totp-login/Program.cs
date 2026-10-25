// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Buffers.Binary;
using System.Globalization;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Site = "https://authenticationtest.com";
// The practice site publishes its test account and TOTP secret on the challenge page.
const string Email = "totp@authenticationtest.com", Password = "pa$$w0rd", Secret = "I65VU7K5ZQL7WB4E";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

var output = new JsonObject { ["site"] = $"{Site}/totpChallenge/", ["session_ids"] = new JsonObject() };
string state;
var (firstId, browser) = await Launch();
output["session_ids"]!["first"] = firstId;
try
{
    // What matters is your clock against the server's. The site's Date header (from its CDN
    // edge, 1 s resolution) should fall between our clock just before and just after the
    // request. The remote browser's clock is recorded for comparison only.
    var page = await browser.NewPageAsync();
    var t0 = Now();
    var resp = await page.GotoAsync($"{Site}/totpChallenge/", new() { Timeout = 60000 });
    var t1 = Now();
    var server = DateTimeOffset.ParseExact(resp!.Headers["date"], "r", CultureInfo.InvariantCulture).ToUnixTimeSeconds();
    output["clocks"] = new JsonObject
    {
        ["server_date_within_request"] = server >= Math.Floor(t0) - 1 && server <= t1 + 1,
        ["request_s"] = Math.Round(t1 - t0, 1),
        ["browser_minus_local_s"] = Math.Round(await page.EvaluateAsync<double>("Date.now()") / 1000 - Now(), 1),
    };
    await page.CloseAsync();

    var attempts = new JsonArray();
    output["attempts"] = attempts;
    foreach (var (label, offset) in new (string, int?)[] {
        ("wrong code", null),
        ("code from 2 windows ago (-60 s)", -60),
        ("code from the previous window (-30 s)", -30),
        ("code from the next window (+30 s)", 30) })
    {
        var a = await Attempt(browser, label, offset);
        attempts.Add(a.Result);
        await a.Context.CloseAsync();
    }
    var current = await Attempt(browser, "current code", 0);
    attempts.Add(current.Result);
    state = await current.Context.StorageStateAsync();
    var cookies = JsonNode.Parse(state)!["cookies"]!.AsArray();
    output["storage_state"] = new JsonObject
    {
        ["cookies"] = cookies.Count,
        ["session_cookies"] = cookies.Count(c => c!["expires"]!.GetValue<double>() == -1),
    };
}
finally
{
    await browser.CloseAsync();
}

// A new session (another browser): without the saved state, then with it.
var (secondId, second) = await Launch();
output["session_ids"]!["second"] = secondId;
try
{
    var secondSession = new JsonObject();
    foreach (var (label, options) in new (string, BrowserNewContextOptions)[] { ("without_state", new()), ("with_state", new() { StorageState = state }) })
    {
        var context = await second.NewContextAsync(options);
        var page = await context.NewPageAsync();
        await page.GotoAsync($"{Site}/", new() { Timeout = 60000 });
        secondSession[label] = new JsonObject { ["signed_in"] = await page.Locator("text=Sign Out").CountAsync() > 0 };
        await context.CloseAsync();
    }
    output["second_session"] = secondSession;
}
finally
{
    await second.CloseAsync();
}
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

async Task<(string Id, IBrowser Browser)> Launch()
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var session = await res.Content.ReadFromJsonAsync<JsonElement>();
    var b = await playwright.Chromium.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    return (session.GetProperty("sessionId").GetString()!, b);
}

// One login attempt in a fresh context (no cookies from earlier attempts). The code is made
// after the form has loaded, right before typing it: offset null types a wrong code, otherwise
// the code for now + offset seconds.
// The challenge page or the post-submit navigation occasionally hangs through a rotating proxy:
// one more try in a fresh context, with the code made again after the reload.
static async Task<(IBrowserContext Context, JsonObject Result)> Attempt(IBrowser browser, string label, int? offset)
{
    try { return await AttemptOnce(browser, label, offset); }
    catch (System.TimeoutException)
    {
        foreach (var c in browser.Contexts.ToList()) { try { await c.CloseAsync(); } catch { } }
        return await AttemptOnce(browser, label, offset);
    }
}

static async Task<(IBrowserContext Context, JsonObject Result)> AttemptOnce(IBrowser browser, string label, int? offset)
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    await page.GotoAsync($"{Site}/totpChallenge/", new() { Timeout = 60000 });
    var window = await FreshWindow();
    var code = offset is null ? "000000" : Totp(Secret, Now() + offset.Value);
    var t = DateTime.UtcNow;
    await page.FillAsync("#email", Email);
    await page.FillAsync("#password", Password);
    await page.FillAsync("#totpmfa", code);
    await page.ClickAsync("input[type=submit]");
    await page.WaitForURLAsync(new Regex("/login(Success|Fail)/"), new() { Timeout = 60000 });
    var landed = new Uri(page.Url).AbsolutePath;
    return (context, new JsonObject
    {
        ["attempt"] = label,
        ["offset_s"] = offset,
        ["seconds_left_in_window"] = window.SecondsLeft,
        ["waited_s"] = window.WaitedS,
        ["landed"] = landed,
        ["accepted"] = landed == "/loginSuccess/",
        ["ms"] = (long)(DateTime.UtcNow - t).TotalMilliseconds,
    });
}

static double Now() => DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() / 1000.0;

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps) from the base32 secret — what an authenticator app does.
static string Totp(string secret, double unixSeconds)
{
    const string alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    var key = new List<byte>();
    int buffer = 0, bits = 0;
    foreach (var c in secret.TrimEnd('='))
    {
        buffer = (buffer << 5) | alphabet.IndexOf(c);
        bits += 5;
        if (bits >= 8) { key.Add((byte)(buffer >> (bits - 8))); bits -= 8; }
    }
    var counter = new byte[8];
    BinaryPrimitives.WriteInt64BigEndian(counter, (long)Math.Floor(unixSeconds / 30));
    var h = HMACSHA1.HashData(key.ToArray(), counter);
    var o = h[^1] & 15;
    var value = (BinaryPrimitives.ReadInt32BigEndian(h.AsSpan(o, 4)) & 0x7fffffff) % 1000000;
    return value.ToString("D6");
}

// A code typed in the last seconds of its window can expire before the server checks it:
// wait for the next window when fewer than 5 s are left.
static async Task<(double SecondsLeft, double WaitedS)> FreshWindow()
{
    var left = 30 - Now() % 30;
    if (left < 5) await Task.Delay(TimeSpan.FromSeconds(left + 0.2));
    return (Math.Round(left, 1), left < 5 ? Math.Round(left, 1) : 0);
}
