// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Api = "https://cdpfleet.com";
// Twelve light, public catalogue pages: what a crawler works through.
var urls = Enumerable.Range(1, 12).Select(i => $"https://books.toscrape.com/catalogue/page-{i}.html").ToArray();
const int Pool = 4;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<(string id, IBrowser browser)> Launch()
{
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
        new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new", record = false });
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var s = await res.Content.ReadFromJsonAsync<JsonElement>();
    var b = await playwright.Chromium.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    return (s.GetProperty("sessionId").GetString()!, b);
}

// How a session ended, from the account API (it appears a few seconds after the end).
async Task<string?> EndReason(string id)
{
    for (var i = 0; i < 30; i++)
    {
        var j = await http.GetFromJsonAsync<JsonElement>($"{Api}/v1/me/sessions?limit=50");
        foreach (var s in j.GetProperty("sessions").EnumerateArray())
            if (s.GetProperty("session_id").GetString() == id && s.TryGetProperty("ended_at", out var e) && e.ValueKind != JsonValueKind.Null)
                return s.GetProperty("end_reason").GetString();
        await Task.Delay(3000);
    }
    return null;
}

async Task<bool> Load(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(url, new() { WaitUntil = WaitUntilState.DOMContentLoaded, Timeout = 45000 }); return true; }
        catch (PlaywrightException) { if (attempt == 2) return false; }
    }
}

var sub = (await http.GetFromJsonAsync<JsonElement>($"{Api}/v1/me")).GetProperty("subscription");
var maxPages = sub.GetProperty("features").GetProperty("max_pages").GetInt32();
var memoryMb = sub.GetProperty("features").GetProperty("memory_mb").GetInt32();
var plan = new JsonObject { ["kind"] = sub.GetProperty("kind").GetString(), ["max_pages"] = maxPages, ["memory_mb"] = memoryMb };

// 1 + 2: tabs. A new tab per URL, never closed, runs into the tab cap; a small pool of reused
// tabs works through the same list without ever getting near it.
JsonObject tabCap, pool;
double heapMb;
var (_, tabs) = await Launch();
try
{
    var ctx = await tabs.NewContextAsync();
    var opened = new List<IPage>();
    int? closedAt = null;
    for (var i = 0; i <= maxPages; i++)
    {
        // Over the cap the node closes the new tab at once: NewPageAsync() fails, or the tab is already closed.
        IPage? p = null;
        try { p = await ctx.NewPageAsync(); await Task.Delay(1500); } catch (PlaywrightException) { }
        if (p == null || p.IsClosed) { closedAt = i + 1; break; }
        await p.SetContentAsync($"<title>tab {i + 1}</title>");
        opened.Add(p);
    }
    var alive = tabs.IsConnected && await opened[0].TitleAsync() == "tab 1";
    tabCap = new JsonObject { ["opened_before_cap"] = opened.Count, ["closed_at_tab"] = closedAt, ["session_alive"] = alive };
    foreach (var p in opened) await p.CloseAsync();

    var pages = new List<IPage>();
    for (var i = 0; i < Pool; i++) pages.Add(await ctx.NewPageAsync());
    var loaded = 0;
    for (var i = 0; i < urls.Length; i += Pool)
    {
        var batch = await Task.WhenAll(urls.Skip(i).Take(Pool).Select((u, j) => Load(pages[j], u)));
        loaded += batch.Count(x => x);
    }
    // JS heap of a loaded catalogue page (Chromium's performance.memory), averaged over the pool.
    var heaps = new List<double>();
    foreach (var p in pages) heaps.Add(await p.EvaluateAsync<double>("() => (performance.memory && performance.memory.usedJSHeapSize) || 0"));
    heapMb = Math.Round(heaps.Average() / 1048576, 1);
    pool = new JsonObject { ["pool_size"] = Pool, ["urls"] = urls.Length, ["loaded"] = loaded, ["open_tabs"] = ctx.Pages.Count };
    foreach (var p in pages) await p.CloseAsync();
}
finally { await tabs.CloseAsync(); }

// 3: memory. Half the cap is fine; growing a page past the cap ends the session.
var (memId, mem) = await Launch();
int mb = 0; bool halfOk = false, cut = false;
try
{
    var page = await mem.NewPageAsync();
    await page.EvaluateAsync("() => { window.hog = []; }");
    try
    {
        while (mb < memoryMb + 1024)
        {
            await page.EvaluateAsync("() => { const a = new Uint8Array(128 << 20); for (let i = 0; i < a.length; i += 4096) a[i] = 1; window.hog.push(a); }");
            mb += 128;
            if (mb >= memoryMb / 2 && !halfOk) halfOk = mem.IsConnected;
        }
    }
    catch (Exception) { cut = true; }
}
finally { try { await mem.CloseAsync(); } catch (Exception) { } }
var memory = new JsonObject { ["half_cap_ok"] = halfOk, ["cut"] = cut, ["allocated_mb_at_cut"] = mb, ["end_reason"] = await EndReason(memId) };

Console.WriteLine(new JsonObject
{
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"), ["plan"] = plan, ["tab_cap"] = tabCap, ["pool"] = pool,
    ["js_heap_mb_per_tab"] = heapMb, ["memory"] = memory,
}.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
