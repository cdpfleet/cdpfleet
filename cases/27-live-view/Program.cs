// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

var output = new JsonArray
{
    await Watch("chromium", "new"),
    await Watch("firefox", true),
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

async Task<JsonObject> Watch(string engine, object headless)
{
    HttpResponseMessage res = null!;
    for (var attempt = 1; attempt <= 5; attempt++) // 503 = momentarily no capacity for this browser
    {
        res = await http.PostAsJsonAsync($"https://starter.cdpfleet.com/{engine}/session",
            new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless });
        if ((int)res.StatusCode != 503) break;
        await Task.Delay(2000 * attempt);
    }
    if (!res.IsSuccessStatusCode)
        return new JsonObject { ["browser"] = engine, ["error"] = $"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var type = engine == "chromium" ? playwright.Chromium : playwright.Firefox;
    var browser = await type.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        var frames = new List<(string Phase, int Bytes, bool Jpeg, int W, int H, byte[] Data)>(); // every frame, tagged with its phase
        var gate = new object();
        var phase = "load";
        // The 1.60 .NET client leaves ScreencastFrame.ViewportWidth/Height at 0; fall back to the page's viewport.
        var viewport = page.ViewportSize;
        // Every frame arrives here as JPEG bytes — this is where a viewer would get it.
        await page.Screencast.StartAsync(new ScreencastStartOptions
        {
            Quality = 60,
            OnFrame = frame =>
            {
                var data = frame.Data;
                lock (gate) frames.Add((Volatile.Read(ref phase), data.Length, data.Length > 1 && data[0] == 0xff && data[1] == 0xd8, frame.ViewportWidth > 0 ? frame.ViewportWidth : viewport?.Width ?? 0, frame.ViewportHeight > 0 ? frame.ViewportHeight : viewport?.Height ?? 0, data));
                return Task.CompletedTask;
            },
        });
        for (var attempt = 1; ; attempt++) // the proxy can drop a tunnel; retry
        {
            try { await page.GotoAsync("https://en.wikipedia.org/wiki/Web_browser", new() { Timeout = 60000 }); break; }
            catch (Exception e) when (e is PlaywrightException or TimeoutException) { if (attempt == 3) throw; }
        }
        Volatile.Write(ref phase, "scroll");
        var sw = Stopwatch.StartNew();
        for (var i = 0; i < 10; i++)
        {
            await page.Mouse.WheelAsync(0, 500);
            await page.WaitForTimeoutAsync(400);
        }
        var scrolling = sw.Elapsed.TotalSeconds;
        Volatile.Write(ref phase, "idle"); // nothing changes on the page now
        await page.WaitForTimeoutAsync(3000);
        await page.Screencast.StopAsync();

        List<(string Phase, int Bytes, bool Jpeg, int W, int H, byte[] Data)> got;
        lock (gate) got = frames.ToList();
        int Count(string p) => got.Count(f => f.Phase == p);
        var last = got.Count > 0 ? got[^1] : default;
        if (got.Count > 0) await File.WriteAllBytesAsync(Path.Combine(Path.GetTempPath(), $"cdpfleet-live-{engine}.jpg"), last.Data);
        return new JsonObject
        {
            ["browser"] = engine,
            ["frames_while_loading"] = Count("load"),
            ["frames_while_scrolling"] = Count("scroll"),
            ["fps_while_scrolling"] = Math.Round(Count("scroll") / scrolling, 1, MidpointRounding.AwayFromZero),
            ["frames_in_3s_idle"] = Count("idle"),
            ["avg_kb"] = (long)Math.Floor((double)got.Sum(f => (long)f.Bytes) / Math.Max(got.Count, 1) / 1024 + 0.5),
            ["all_jpeg"] = got.All(f => f.Jpeg),
            ["viewport"] = got.Count > 0 ? $"{last.W}x{last.H}" : null,
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}
