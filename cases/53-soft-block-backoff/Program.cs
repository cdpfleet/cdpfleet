// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, SOCKS_PROXIES (comma-separated sticky socks5:// URLs: one exit IP each)
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxies = Environment.GetEnvironmentVariable("SOCKS_PROXIES")!.Split(',');
const string Challenge = "<title>Just a moment...</title><p>Checking your browser before accessing the site.</p>";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// A stand-in for a rate-limited site, in front of real httpbin requests. Each request really goes
// out through the session's proxy; the limiter decides by the IP httpbin saw (its "origin"):
// 3 pages in a burst, then one page per 5 s (429 + Retry-After: 5), 6 pages per IP in total. An IP
// that comes back before its Retry-After, or is over its quota, gets a challenge page from then on.
Func<IRoute, Task> Limiter()
{
    var ips = new Dictionary<string, IpState>();
    return async (route) =>
    {
        var now = DateTime.UtcNow; // when the request arrived
        IAPIResponse real;
        try { real = await route.FetchAsync(new() { Timeout = 30000 }); }
        catch (PlaywrightException) { await route.FulfillAsync(new() { Status = 502, ContentType = "text/plain", Body = "Bad Gateway" }); return; } // the proxy hiccuped
        if (real.Status != 200) { await route.FulfillAsync(new() { Response = real }); return; } // httpbin itself failed
        var ip = (await real.JsonAsync())!.Value.GetProperty("origin").GetString()!;
        if (!ips.TryGetValue(ip, out var s)) ips[ip] = s = new IpState();
        if (now < s.RetryAt || s.Served >= 6) s.Challenged = true;
        if (s.Challenged) { await route.FulfillAsync(new() { Status = 403, ContentType = "text/html", Body = Challenge }); return; }
        if (s.Served >= 3 && now - s.Last < TimeSpan.FromSeconds(5))
        {
            s.RetryAt = now.AddSeconds(5);
            await route.FulfillAsync(new() { Status = 429, Headers = new Dictionary<string, string> { ["retry-after"] = "5" }, ContentType = "text/plain", Body = "Too Many Requests" });
            return;
        }
        s.Served++;
        s.Last = now;
        await route.FulfillAsync(new() { Response = real });
    };
}

// What the response means: the data, a rate limit (wait), or a block (this IP is done).
string Classify(int status, string text) =>
    status == 429 ? "rate_limited"
    : status is 403 or 503 || Regex.IsMatch(text, "just a moment|captcha|access denied", RegexOptions.IgnoreCase) ? "blocked"
    : status == 200 ? "ok" : "error";

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync("https://starter.cdpfleet.com/chromium/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

async Task<JsonObject> Crawl(bool polite)
{
    var session = await Launch(new JsonObject { ["proxy"] = proxies[polite ? 1 : 0], ["headless"] = "new", ["record"] = false });
    var wsUrl = session.GetProperty("wsUrl").GetString()!;
    var sessionId = session.GetProperty("sessionId").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var site = Limiter();

    // Live proxy swap on the session's router: new connections use the new proxy.
    async Task SwapProxy(string proxy)
    {
        var r = await http.PostAsJsonAsync($"https://{new Uri(wsUrl).Host}/admin/session/proxy", new { session_id = sessionId, proxy });
        if (!r.IsSuccessStatusCode) throw new Exception($"swap {(int)r.StatusCode} {await r.Content.ReadAsStringAsync()}");
    }
    async Task<(IBrowserContext, IPage)> Open(string? storageState = null)
    {
        var context = await browser.NewContextAsync(new() { StorageState = storageState });
        await context.RouteAsync("https://httpbin.org/anything/**", site);
        return (context, await context.NewPageAsync());
    }

    var rows = new JsonArray();
    var exits = new List<string>();
    int waited = 0, swaps = 0, next = 2, fetched = 0;
    bool cookieKept;
    try
    {
        var (context, page) = await Open();
        // A logged-in state that must survive a proxy swap.
        await page.GotoAsync("https://httpbin.org/cookies/set?session=alice-42", new() { Timeout = 60000 });
        for (var i = 0; i < 8; i++)
        {
            var trail = new List<string>();
            string outcome = "gave up";
            string? exitIp = null;
            for (var attempt = 1; attempt <= 6; attempt++)
            {
                var res = (await page.GotoAsync($"https://httpbin.org/anything/catalogue/page-{i + 1}", new() { Timeout = 60000 }))!;
                var text = await page.TextContentAsync("body") ?? "";
                var kind = Classify(res.Status, text);
                trail.Add(res.Status.ToString());
                if (kind == "ok")
                {
                    outcome = "ok";
                    exitIp = JsonDocument.Parse(text).RootElement.GetProperty("origin").GetString();
                    break;
                }
                if (kind == "rate_limited")
                {
                    if (!polite && attempt == 3) break;
                    // Honour Retry-After; without one, back off exponentially (1, 2, 4… s, capped).
                    var seconds = !polite ? 0 : res.Headers.TryGetValue("retry-after", out var ra) ? int.Parse(ra) : (int)Math.Min(Math.Pow(2, attempt - 1), 30);
                    if (seconds > 0) trail.Add($"wait {seconds}s");
                    waited += seconds;
                    await Task.Delay(seconds * 1000);
                    continue;
                }
                if (kind == "blocked" && polite && next < proxies.Length)
                {
                    // This exit is done: move the session to another proxy and continue in a new context
                    // (its own connections) that keeps the cookies and storage.
                    await SwapProxy(proxies[next++]);
                    var state = await context.StorageStateAsync();
                    await context.CloseAsync();
                    (context, page) = await Open(state);
                    swaps++;
                    trail.Add("swap proxy");
                    continue;
                }
                if (kind == "error")
                {
                    await Task.Delay(1000); // a transient failure: try again
                    continue;
                }
                break;
            }
            if (exitIp != null)
            {
                fetched++;
                if (!exits.Contains(exitIp)) exits.Add(exitIp);
            }
            rows.Add(new JsonObject { ["page"] = i + 1, ["outcome"] = outcome, ["exit_ip"] = exitIp, ["trail"] = string.Join(" → ", trail) });
        }
        await page.GotoAsync("https://httpbin.org/cookies", new() { Timeout = 60000 });
        var cookies = JsonDocument.Parse(await page.TextContentAsync("body") ?? "{}").RootElement.GetProperty("cookies");
        cookieKept = cookies.TryGetProperty("session", out var c) && c.GetString() == "alice-42";
    }
    finally
    {
        await browser.CloseAsync();
    }
    return new JsonObject
    {
        ["client"] = polite ? "polite" : "naive",
        ["pages_fetched"] = fetched,
        ["exits"] = new JsonArray(exits.Select((e) => (JsonNode)JsonValue.Create(e)!).ToArray()),
        ["waited_seconds"] = waited,
        ["proxy_swaps"] = swaps,
        ["session_cookie_kept"] = cookieKept,
        ["pages"] = rows,
    };
}

// Naive: retries a 429 straight away and has no plan for a block. Polite: waits as told and moves
// to another exit when one is blocked.
var naive = await Crawl(false);
var polite = await Crawl(true);
Console.WriteLine(new JsonObject { ["naive"] = naive, ["polite"] = polite }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

class IpState
{
    public int Served;
    public DateTime Last;
    public DateTime RetryAt;
    public bool Challenged;
}
