// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
const string Site = "https://www.httpbin.org";

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

async Task<IBrowser> Open(string engine)
{
    var session = await Launch(engine, new JsonObject { ["proxy"] = proxy, ["headless"] = true, ["record"] = false });
    var type = engine switch { "firefox" => playwright.Firefox, "webkit" => playwright.Webkit, _ => playwright.Chromium };
    return await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
}

// httpbin answers 502/503 now and then: retry a page that isn't JSON.
async Task<JsonObject> Json(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        await page.GotoAsync(url, new() { Timeout = 45000 });
        try { return JsonNode.Parse((await page.TextContentAsync("body"))!)!.AsObject(); }
        catch (JsonException) when (attempt < 4) { await Task.Delay(3000); }
    }
}

JsonArray Names(IEnumerable<BrowserContextCookiesResult> cookies) => new(cookies.Select(c => c.Name).Order(StringComparer.Ordinal).Select(n => (JsonNode)n!).ToArray());

// Part 1: inspect, edit, clear and carry the jar (Chromium).
var jar = new JsonObject();
string saved;
var browser = await Open("chromium");
try
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    // The site sets two cookies (no expiry: session cookies); another site sets one more.
    await Json(page, $"{Site}/cookies/set?session_token=abc123&theme=dark");
    await context.AddCookiesAsync(new[] { new Cookie { Name = "other_site", Value = "1", Url = "https://example.com" } });
    jar["all_cookies"] = Names(await context.CookiesAsync());
    var mine = await context.CookiesAsync(new[] { Site }); // only the cookies that would be sent to this URL
    jar["for_site"] = Names(mine);
    var token = mine.First(c => c.Name == "session_token");
    var theme = mine.First(c => c.Name == "theme");
    jar["session_cookie_expires"] = (long)token.Expires; // -1 = session cookie

    // Edit: AddCookiesAsync with the same name, domain and path replaces the cookie.
    var weekFromNow = DateTimeOffset.UtcNow.ToUnixTimeSeconds() + 7 * 86400;
    Cookie Copy(BrowserContextCookiesResult c) => new() { Name = c.Name, Value = c.Value, Domain = c.Domain, Path = c.Path, Expires = c.Expires, HttpOnly = c.HttpOnly, Secure = c.Secure, SameSite = c.SameSite };
    var editedToken = Copy(token); editedToken.Expires = weekFromNow;
    var editedTheme = Copy(theme); editedTheme.Value = "light";
    await context.AddCookiesAsync(new[] { editedToken, editedTheme });
    var expires = (await context.CookiesAsync(new[] { Site })).First(c => c.Name == "session_token").Expires;
    jar["token_expires_in_days"] = (long)Math.Round((expires - DateTimeOffset.UtcNow.ToUnixTimeSeconds()) / 86400.0);
    jar["server_sees_after_edit"] = (await Json(page, $"{Site}/cookies"))["cookies"]!.DeepClone();

    // Clear one cookie by name (other filters: domain, path).
    await context.ClearCookiesAsync(new() { Name = "theme" });
    jar["after_clear_by_name"] = Names(await context.CookiesAsync());

    // Carry the login as plain JSON: just the site's cookies.
    saved = JsonSerializer.Serialize((await context.CookiesAsync(new[] { Site })).Select(Copy));
    jar["saved_json_bytes"] = saved.Length;
}
finally { await browser.CloseAsync(); }

browser = await Open("chromium"); // a new session on another browser
try
{
    var context = await browser.NewContextAsync();
    jar["new_session_before"] = Names(await context.CookiesAsync());
    await context.AddCookiesAsync(JsonSerializer.Deserialize<List<Cookie>>(saved)!);
    var page = await context.NewPageAsync();
    jar["new_session_server_sees"] = (await Json(page, $"{Site}/cookies"))["cookies"]!.DeepClone();
}
finally { await browser.CloseAsync(); }

// Part 2: the same three ways to set a cookie, on three hosts, in three engines.
// httpbin.org and postman-echo.com are on the Public Suffix List; www.httpbin.org isn't.
var psl = new JsonArray();
foreach (var engine in new[] { "chromium", "firefox", "webkit" })
{
    browser = await Open(engine);
    try
    {
        foreach (var host in new[] { "httpbin.org", "www.httpbin.org", "postman-echo.com" })
        {
            var context = await browser.NewContextAsync();
            var page = await context.NewPageAsync();
            var url = $"https://{host}";
            await context.AddCookiesAsync(new[] { new Cookie { Name = "via_add_cookies", Value = "1", Url = url } });
            await Json(page, $"{url}/cookies/set?via_set_cookie=1"); // Set-Cookie, then a redirect to /cookies
            await page.EvaluateAsync("() => { document.cookie = 'via_document_cookie=1; path=/; max-age=3600'; }");
            var kept = (await context.CookiesAsync(new[] { url })).Select(c => c.Name).ToHashSet();
            var server = (await Json(page, $"{url}/cookies"))["cookies"]!.AsObject().Select(p => p.Key).Order(StringComparer.Ordinal);
            psl.Add(new JsonObject
            {
                ["engine"] = engine,
                ["host"] = host,
                ["add_cookies"] = kept.Contains("via_add_cookies"),
                ["set_cookie"] = kept.Contains("via_set_cookie"),
                ["document_cookie"] = kept.Contains("via_document_cookie"),
                ["server_sees"] = new JsonArray(server.Select(n => (JsonNode)n!).ToArray()),
            });
            await context.CloseAsync();
        }
    }
    finally { await browser.CloseAsync(); }
}

Console.WriteLine(new JsonObject { ["jar"] = jar, ["psl"] = psl }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
