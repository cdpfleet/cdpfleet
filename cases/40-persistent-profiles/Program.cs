// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Buffers.Binary;
using System.Diagnostics;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var name = $"case40-csharp-{Convert.ToHexString(RandomNumberGenerator.GetBytes(4)).ToLowerInvariant()}"; // one profile per run
const string Practice = "https://authenticationtest.com";
const string Site = "https://httpbin.org/html";
// The practice site publishes its test account and TOTP secret on the challenge page.
const string Email = "totp@authenticationtest.com", Password = "pa$$w0rd", Secret = "I65VU7K5ZQL7WB4E";
string[] storageKeys = ["persistent_cookie", "session_cookie", "local_storage", "session_storage", "indexeddb"];

// Writes every kind of storage on httpbin.org, and reads it back.
const string WriteJs = """
async () => {
  document.cookie = 'remember_me=alice; max-age=2592000; path=/';  // what "Remember me" sets
  document.cookie = 'session_only=yes; path=/';                    // no expiry: a session cookie
  localStorage.setItem('theme', 'dark');
  sessionStorage.setItem('draft', 'unsaved');
  await new Promise((resolve, reject) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const tx = open.result.transaction('auth', 'readwrite');
      tx.objectStore('auth').put({ email: 'alice@example.com', token: 'demo-token' }, 'user');
      tx.oncomplete = () => { open.result.close(); resolve(); };
      tx.onerror = () => reject(tx.error);
    };
  });
}
""";
const string ReadJs = """
async () => {
  const cookies = Object.fromEntries(document.cookie.split('; ').filter(Boolean).map((c) => c.split('=')));
  const idb = await new Promise((resolve) => {
    const open = indexedDB.open('app', 1);
    open.onupgradeneeded = () => open.result.createObjectStore('auth');
    open.onsuccess = () => {
      const db = open.result;
      if (!db.objectStoreNames.contains('auth')) { db.close(); resolve(null); return; }
      const get = db.transaction('auth').objectStore('auth').get('user');
      get.onsuccess = () => { db.close(); resolve(get.result ?? null); };
      get.onerror = () => { db.close(); resolve(null); };
    };
    open.onerror = () => resolve(null);
  });
  return {
    persistent_cookie: cookies.remember_me === 'alice',
    session_cookie: cookies.session_only === 'yes',
    local_storage: localStorage.getItem('theme') === 'dark',
    session_storage: sessionStorage.getItem('draft') === 'unsaved',
    indexeddb: idb?.email === 'alice@example.com',
  };
}
""";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

var output = new JsonObject
{
    ["profile_name_prefix"] = "case40-", ["sessions"] = new JsonObject(), ["first"] = new JsonObject(), ["saved"] = new JsonObject(),
    ["second"] = new JsonObject(), ["new_context"] = new JsonObject(), ["second_writer"] = new JsonObject(), ["readonly"] = new JsonArray(), ["after"] = new JsonObject(),
};
var first = output["first"]!.AsObject();
var second = output["second"]!.AsObject();
var after = output["after"]!.AsObject();
var readonlyList = output["readonly"]!.AsArray();
try
{
    // Session A: a new profile. Log in (TOTP) and write every kind of storage.
    var a = await Attach(await Start(name));
    if (a.Browser is null) throw new Exception($"launch A {a.Status} {a.Error}");
    output["sessions"]!["a"] = a.SessionId;
    first["launch_ms"] = a.LaunchMs;
    try
    {
        await Go(a.Page!, $"{Practice}/totpChallenge/");
        if (30 - Now() % 30 < 5) await Task.Delay(5200); // fresh TOTP window
        await a.Page!.FillAsync("#email", Email);
        await a.Page.FillAsync("#password", Password);
        await a.Page.FillAsync("#totpmfa", Totp(Secret, Now()));
        await a.Page.ClickAsync("input[type=submit]");
        await a.Page.WaitForURLAsync(new Regex("/login(Success|Fail)/"), new() { Timeout = 60000 });
        first["practice_login"] = new Uri(a.Page.Url).AbsolutePath == "/loginSuccess/";
        first["practice_cookie_expires"] = new JsonArray((await a.Context!.CookiesAsync([Practice]))
            .Select(c => (JsonNode)(c.Expires == -1 ? "session" : "persistent")).ToArray());
        await Go(a.Page, Site);
        await a.Page.EvaluateAsync(WriteJs);
        first["wrote"] = await Storage(a.Page);
    }
    finally
    {
        await a.Browser.CloseAsync(); // ends the session; the node saves the profile once the browser has exited
    }
    var saved = await WaitForVersion(1);
    output["saved"] = new JsonObject { ["version"] = saved["version"]!.GetValue<int>(), ["bytes"] = saved["bytes"]!.GetValue<long>() };

    // Session B: the same profile, restored.
    var b = await Attach(await Start(name));
    if (b.Browser is null) throw new Exception($"launch B {b.Status} {b.Error}");
    output["sessions"]!["b"] = b.SessionId;
    second["launch_ms"] = b.LaunchMs;
    try
    {
        second["restored"] = await Storage(b.Page!);
        second["practice_signed_in"] = await SignedIntoPractice(b.Page!);

        // The trap: a new context in the same session starts empty and is never saved.
        var scratch = await b.Browser.NewContextAsync();
        output["new_context"] = await Storage(await scratch.NewPageAsync());
        await scratch.CloseAsync();

        // While B holds the profile: a second normal launch is refused, read-only ones work.
        var w = await Attach(await Start(name));
        output["second_writer"] = new JsonObject { ["status"] = w.Status, ["error"] = w.Error };
        if (w.Browser is not null) await w.Browser.CloseAsync();
        // Launch the readers in parallel (HTTP), then drive each one.
        var launches = await Task.WhenAll(Enumerable.Range(0, 3).Select(_ => Start(new JsonObject { ["id"] = name, ["mode"] = "readonly" })));
        foreach (var started in launches)
        {
            var r = await Attach(started);
            if (r.Browser is null) { readonlyList.Add(new JsonObject { ["status"] = r.Status, ["error"] = r.Error }); continue; }
            try
            {
                var s = await Storage(r.Page!);
                readonlyList.Add(new JsonObject
                {
                    ["status"] = r.Status, ["persistent_cookie"] = s["persistent_cookie"]!.GetValue<bool>(),
                    ["local_storage"] = s["local_storage"]!.GetValue<bool>(), ["indexeddb"] = s["indexeddb"]!.GetValue<bool>(),
                });
            }
            finally
            {
                await r.Browser.CloseAsync();
            }
        }
        await b.Page!.EvaluateAsync("() => document.title"); // keep B active (idle sessions end after 60 s on shared threads)
        after["version_after_readonly"] = (await ProfileInfo())?["version"]?.GetValue<int>();
    }
    finally
    {
        await b.Browser.CloseAsync();
    }
    after["version_after_b"] = (await WaitForVersion(2))["version"]!.GetValue<int>();
}
finally
{
    // Delete the profile once no session holds it.
    for (var i = 0; i < 60 && (await ProfileInfo())?["in_use"]?.GetValue<bool>() == true; i++) await Task.Delay(2000);
    var del = await http.DeleteAsync($"https://cdpfleet.com/v1/me/profiles/{name}");
    after["deleted"] = (int)del.StatusCode == 200 && await ProfileInfo() is null;
}
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

// RFC 6238 TOTP (SHA-1, 6 digits, 30 s steps), as in the TOTP login case.
static string Totp(string secret, double unixSeconds)
{
    const string alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
    var key = new List<byte>();
    int buffer = 0, bits = 0;
    foreach (var c in secret)
    {
        buffer = (buffer << 5) | alphabet.IndexOf(c);
        bits += 5;
        if (bits >= 8) { key.Add((byte)(buffer >> (bits - 8))); bits -= 8; }
    }
    var counter = new byte[8];
    BinaryPrimitives.WriteInt64BigEndian(counter, (long)Math.Floor(unixSeconds / 30));
    var h = HMACSHA1.HashData(key.ToArray(), counter);
    var o = h[^1] & 15;
    return ((BinaryPrimitives.ReadInt32BigEndian(h.AsSpan(o, 4)) & 0x7fffffff) % 1000000).ToString("D6");
}

static double Now() => DateTimeOffset.UtcNow.ToUnixTimeMilliseconds() / 1000.0;

async Task<JsonNode?> ProfileInfo()
{
    var list = await http.GetFromJsonAsync<JsonObject>("https://cdpfleet.com/v1/me/profiles");
    return list!["profiles"]!.AsArray().FirstOrDefault(p => p!["name"]!.GetValue<string>() == name);
}

// The node saves the profile after the session's browser has exited: wait for the new version.
async Task<JsonNode> WaitForVersion(int version)
{
    for (var i = 0; i < 60; i++)
    {
        var p = await ProfileInfo();
        if (p is not null && p["version"]!.GetValue<int>() >= version && !p["in_use"]!.GetValue<bool>()) return p;
        await Task.Delay(2000);
    }
    throw new Exception($"profile {name} never reached version {version}");
}

// POST the launch with the profile.
async Task<(int Status, JsonObject Body, Stopwatch Clock)> Start(JsonNode profile)
{
    var clock = Stopwatch.StartNew();
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
        new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = true, ["profile"] = profile });
    return ((int)res.StatusCode, (await res.Content.ReadFromJsonAsync<JsonObject>())!, clock);
}

// Connect to a launched session and open a page in the profile's context.
async Task<Attached> Attach((int Status, JsonObject Body, Stopwatch Clock) s)
{
    if (s.Status != 200) return new Attached(s.Status, s.Body["error"]?.GetValue<string>(), null, null, null, null, 0);
    var browser = await playwright.Chromium.ConnectAsync(s.Body["wsUrl"]!.GetValue<string>(),
        new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var context = browser.Contexts[0]; // the profile — a NewContextAsync() would not be saved
    var page = context.Pages.Count > 0 ? context.Pages[0] : await context.NewPageAsync();
    return new Attached(s.Status, null, s.Body["sessionId"]!.GetValue<string>(), browser, context, page, s.Clock.ElapsedMilliseconds);
}

// Proxies drop a tunnel now and then: up to two retries.
static async Task Go(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(url, new() { Timeout = 60000 }); return; }
        catch (Exception e) when (attempt < 3 && e is PlaywrightException or TimeoutException) { await Task.Delay(2000); }
    }
}

// Read by key: Playwright .NET adds "$id" metadata to objects it returns.
async Task<JsonObject> Storage(IPage page)
{
    await Go(page, Site);
    var el = await page.EvaluateAsync<JsonElement>(ReadJs);
    var o = new JsonObject();
    foreach (var k in storageKeys) o[k] = el.GetProperty(k).GetBoolean();
    return o;
}

static async Task<bool> SignedIntoPractice(IPage page)
{
    await Go(page, $"{Practice}/");
    return await page.Locator("text=Sign Out").CountAsync() > 0;
}

record Attached(int Status, string? Error, string? SessionId, IBrowser? Browser, IBrowserContext? Context, IPage? Page, long LaunchMs);
