// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY (no proxy of your own needed: the cdpfleet residential proxy, billed per GB by tier)
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string PageUrl = "https://commons.wikimedia.org/wiki/Category:Cats";
var price = new Dictionary<int, double> { [1] = 5, [4] = 0.5 }; // USD per GB (10^9 bytes)
string[] cdn = ["thumb.wikimedia.org", "upload.wikimedia.org"]; // the images; HTML, CSS and JS come from commons.wikimedia.org

// One session, two prices: the page through Tier 1, the image hosts through Tier 4.
var launch = new
{
    headless = "new",
    record = false,
    proxy = "cdpfleet-resi-country-us",
    proxy_rules = new[] { new { hosts = cdn, proxy = "cdpfleet-resi-tier-4-country-us" } },
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
static long Round(double x) => (long)Math.Floor(x + 0.5);

// Today's residential traffic per tier, as billed.
async Task<Dictionary<int, long>> UsedToday()
{
    var b = await http.GetFromJsonAsync<JsonElement>("https://cdpfleet.com/v1/me/proxy-balance");
    return b.GetProperty("used_today").EnumerateArray().ToDictionary(t => t.GetProperty("tier").GetInt32(), t => t.GetProperty("bytes").GetInt64());
}

// Load the gallery, scroll to the end, and add up every request's bytes by host group.
async Task<JsonObject> Load(IPlaywright playwright)
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", launch);
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;
    var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        var done = new List<IRequest>();
        page.RequestFinished += (_, req) => { lock (done) done.Add(req); };
        // Residential peers drop the odd connection: retry the navigation once.
        for (var attempt = 1; ; attempt++)
        {
            try { await page.GotoAsync(PageUrl, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 }); break; }
            catch (Exception e) when (e is PlaywrightException or TimeoutException) { if (attempt == 2) throw; }
        }
        for (var i = 0; i < 20; i++) // lazy-loaded thumbnails load as they scroll into view
        {
            await page.Mouse.WheelAsync(0, 1500);
            await page.WaitForTimeoutAsync(250);
        }
        try { await page.WaitForLoadStateAsync(LoadState.NetworkIdle, new() { Timeout = 15000 }); }
        catch (TimeoutException) { }
        long pageBytes = 0, imageBytes = 0;
        var images = 0;
        IRequest[] finished;
        lock (done) finished = done.ToArray();
        foreach (var req in finished)
        {
            RequestSizesResult s;
            try { s = await req.SizesAsync(); }
            catch (PlaywrightException) { continue; }
            long total = s.RequestHeadersSize + s.RequestBodySize + s.ResponseHeadersSize + s.ResponseBodySize;
            if (cdn.Contains(new Uri(req.Url).Host)) { imageBytes += total; images++; } else pageBytes += total;
        }
        return new JsonObject
        {
            ["requests"] = finished.Length,
            ["image_requests"] = images,
            ["page_kb"] = Round(pageBytes / 1000.0),
            ["image_kb"] = Round(imageBytes / 1000.0),
        };
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var before = await UsedToday();
JsonObject run;
using (var playwright = await Playwright.CreateAsync()) run = await Load(playwright);
var pageKb = (long)run["page_kb"]!;
var imageKb = (long)run["image_kb"]!;

// Metering trails the traffic by about a minute: wait until Tier 4 shows the images.
var after = before;
for (var i = 0; i < 18 && (after.GetValueOrDefault(4) - before.GetValueOrDefault(4)) / 1000.0 < imageKb * 0.8; i++)
{
    await Task.Delay(10000);
    after = await UsedToday();
}

// What 1,000 such loads cost: everything on Tier 1 versus the split.
double Per1000(int pageTier, int imageTier) => Round((pageKb * price[pageTier] + imageKb * price[imageTier]) / 1e6 * 1000 * 100) / 100.0;
var doc = new JsonObject
{
    ["page"] = PageUrl,
    ["run"] = run,
    ["image_share"] = Round((double)imageKb / (imageKb + pageKb) * 100) / 100.0,
    ["cost_per_1000_loads_usd"] = new JsonObject { ["all_tier_1"] = Per1000(1, 1), ["page_tier_1_images_tier_4"] = Per1000(1, 4) },
    ["billed_kb"] = new JsonObject
    {
        ["tier_1"] = Round((after.GetValueOrDefault(1) - before.GetValueOrDefault(1)) / 1000.0),
        ["tier_4"] = Round((after.GetValueOrDefault(4) - before.GetValueOrDefault(4)) / 1000.0),
    },
};
Console.WriteLine(doc.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
