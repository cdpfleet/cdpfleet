// dotnet add package Microsoft.Playwright --version 1.60.0
// env: COMMUNITY_API_KEY
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("COMMUNITY_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync("https://starter.cdpfleet.com/chromium/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// The exit the site sees. Shared proxies drop the odd connection: retry a few times.
async Task<(string Ip, string Country)> ExitOf(IPage page)
{
    for (var i = 1; ; i++)
    {
        try
        {
            await page.GotoAsync("http://ip-api.com/json", new() { Timeout = 30000 });
            var j = JsonDocument.Parse(await page.TextContentAsync("body") ?? "{}").RootElement;
            return (j.GetProperty("query").GetString()!, j.GetProperty("countryCode").GetString()!);
        }
        catch (Exception) when (i < 4)
        {
            await page.WaitForTimeoutAsync(2000 * i);
        }
    }
}

async Task<(int Weight, T Result)> Session<T>(JsonObject extra, Func<IPage, Task<T>> work)
{
    var body = new JsonObject { ["headless"] = true, ["record"] = false };
    foreach (var (k, v) in extra) body[k] = v?.DeepClone();
    var s = await Launch(body);
    var browser = await playwright.Chromium.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        return (s.GetProperty("weight").GetInt32(), await work(await browser.NewPageAsync()));
    }
    finally
    {
        await browser.CloseAsync(); // ends the session and stops the clock
    }
}

// 1. What the free plan includes.
var sub = (await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me")).GetProperty("subscription");
var plan = new JsonObject
{
    ["plan"] = sub.GetProperty("plan_id").GetString(),
    ["threads"] = sub.GetProperty("threads").GetInt32(),
    ["minutes_per_thread_per_day"] = sub.GetProperty("daily_minutes_per_thread").GetInt32(),
    ["max_session_minutes"] = sub.GetProperty("max_session_seconds").GetInt32() / 60,
    ["max_pages"] = sub.GetProperty("features").GetProperty("max_pages").GetInt32(),
    ["memory_mb"] = sub.GetProperty("features").GetProperty("memory_mb").GetInt32(),
    ["proxy_options"] = sub.TryGetProperty("plan_proxy", out var pp) ? JsonNode.Parse(pp.GetProperty("options").GetRawText()) : null,
};

// 2. No proxy in the launch: the plan's included proxy is used. Scrape a catalogue page.
var first = await Session(new JsonObject(), async page =>
{
    var exit = await ExitOf(page);
    await page.GotoAsync("https://books.toscrape.com/", new() { Timeout = 45000 });
    var books = await page.EvalOnSelectorAllAsync<JsonElement>("article.product_pod",
        "els => els.map(e => ({ title: e.querySelector('h3 a').getAttribute('title'), price: e.querySelector('.price_color').textContent.trim() }))");
    return (exit, books);
});

// 3. A cdpfleet-plan token picks a country and keeps the same exit across sessions.
var token = $"cdpfleet-plan-country-de-session-c47{Guid.NewGuid().ToString("N")[..5]}-lifetime-10";
var a = await Session(new JsonObject { ["proxy"] = token }, ExitOf);
var b = await Session(new JsonObject { ["proxy"] = token }, ExitOf);

var output = new JsonObject
{
    ["plan"] = plan,
    ["default_proxy"] = new JsonObject
    {
        ["weight"] = first.Weight,
        ["exit_country"] = first.Result.exit.Country,
        ["exit_ip"] = first.Result.exit.Ip,
        ["books"] = first.Result.books.GetArrayLength(),
        ["first_book"] = JsonNode.Parse(first.Result.books[0].GetRawText()),
    },
    ["plan_token"] = new JsonObject
    {
        ["token"] = Regex.Replace(token, @"session-\w+", "session-…"),
        ["first_country"] = a.Result.Country,
        ["second_country"] = b.Result.Country,
        ["same_ip"] = a.Result.Ip == b.Result.Ip,
    },
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
