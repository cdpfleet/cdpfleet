// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Collections.Concurrent;
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const int Pages = 24;
var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// Launch with the retries the API asks for: 429 (thread limit, launch rate) and 503
// (fleet momentarily busy) carry Retry-After.
async Task<(string WsUrl, long Ms)> Launch(ConcurrentDictionary<string, int> retries)
{
    for (var attempt = 1; ; attempt++)
    {
        var sw = Stopwatch.StartNew();
        var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
            new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
        var body = await res.Content.ReadFromJsonAsync<JsonElement>();
        if (res.IsSuccessStatusCode) return (body.GetProperty("wsUrl").GetString()!, sw.ElapsedMilliseconds);
        var error = body.TryGetProperty("error", out var e) ? e.GetString() ?? "" : "";
        var retryable = (int)res.StatusCode == 503 || ((int)res.StatusCode == 429 && !error.Contains("quota"));
        if (!retryable || attempt == 10) throw new Exception($"launch: {(int)res.StatusCode} {error}");
        retries.AddOrUpdate(error, 1, (_, n) => n + 1);
        await Task.Delay(TimeSpan.FromSeconds(res.Headers.RetryAfter?.Delta?.TotalSeconds ?? 2));
    }
}

// Residential proxies drop a tunnel now and then (ERR_TUNNEL_CONNECTION_FAILED): retry.
async Task<string> Scrape(IPage page, Action retried)
{
    for (var attempt = 1; ; attempt++)
    {
        try
        {
            await page.GotoAsync("https://en.wikipedia.org/wiki/Special:Random", new() { Timeout = 30000 });
            return await page.TitleAsync();
        }
        catch (Exception err) when (err is PlaywrightException or TimeoutException) // .NET times out with TimeoutException
        {
            retried();
            if (attempt == 3) return $"(failed: {err.Message.Split('\n')[0]})";
        }
    }
}

// Runs Pages pages on `workers` parallel workers; each worker either opens one session
// and reuses it, or opens a new session for every page.
async Task<JsonObject> Run(int workers, bool reuse)
{
    var titles = new ConcurrentQueue<string>();
    var retries = new ConcurrentDictionary<string, int>();
    long launches = 0, launchMs = 0, connectMs = 0, sessionMs = 0, pageRetries = 0, connectFailures = 0;
    var next = -1;

    // If the connect fails (rare: the server holding the browser didn't answer), don't
    // reconnect to the same wsUrl — launch a fresh session. Such sessions aren't billed.
    async Task<(IBrowser Browser, Stopwatch Clock)> Open()
    {
        for (var attempt = 1; ; attempt++)
        {
            var (ws, ms) = await Launch(retries);
            Interlocked.Increment(ref launches);
            Interlocked.Add(ref launchMs, ms);
            var clock = Stopwatch.StartNew();
            try
            {
                var browser = await playwright.Chromium.ConnectAsync(ws, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
                Interlocked.Add(ref connectMs, clock.ElapsedMilliseconds);
                return (browser, clock);
            }
            catch (Exception err) when ((err is PlaywrightException or TimeoutException) && attempt < 3)
            {
                Interlocked.Increment(ref connectFailures);
            }
        }
    }
    async Task Close((IBrowser Browser, Stopwatch Clock) s)
    {
        await s.Browser.CloseAsync();
        Interlocked.Add(ref sessionMs, s.Clock.ElapsedMilliseconds);
    }

    async Task Worker()
    {
        if (reuse)
        {
            var s = await Open();
            try
            {
                var page = await s.Browser.NewPageAsync();
                while (Interlocked.Increment(ref next) < Pages) titles.Enqueue(await Scrape(page, () => Interlocked.Increment(ref pageRetries)));
            }
            finally { await Close(s); }
            return;
        }
        while (Interlocked.Increment(ref next) < Pages)
        {
            var s = await Open();
            try { titles.Enqueue(await Scrape(await s.Browser.NewPageAsync(), () => Interlocked.Increment(ref pageRetries))); }
            finally { await Close(s); }
        }
    }

    var wall = Stopwatch.StartNew();
    await Task.WhenAll(Enumerable.Range(0, workers).Select(_ => Worker()));
    return new JsonObject
    {
        ["strategy"] = reuse ? "reuse one session per worker" : "new session per page",
        ["pages"] = titles.Count,
        ["wall_seconds"] = Math.Round(wall.Elapsed.TotalSeconds, 1),
        ["launches"] = launches,
        ["avg_launch_ms"] = launchMs / launches,
        ["avg_connect_ms"] = connectMs / launches,
        ["launch_retries"] = new JsonObject(retries.Select(kv => KeyValuePair.Create(kv.Key, (JsonNode?)kv.Value))),
        ["connect_failures"] = connectFailures,
        ["page_retries"] = pageRetries,
        ["billed_thread_seconds"] = (long)Math.Round(sessionMs / 1000.0),
        ["sample_titles"] = new JsonArray(titles.Take(3).Select(t => (JsonNode?)t).ToArray()),
    };
}

// Size the pool from the plan: never more workers than threads.
var me = JsonNode.Parse(await http.GetStringAsync("https://cdpfleet.com/v1/me"))!;
var threads = (int)me["subscription"]!["threads"]!;
var workerCount = Math.Min(threads, 6);
var result = new JsonObject
{
    ["plan_threads"] = threads,
    ["workers"] = workerCount,
    ["results"] = new JsonArray(await Run(workerCount, false), await Run(workerCount, true)),
};
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
