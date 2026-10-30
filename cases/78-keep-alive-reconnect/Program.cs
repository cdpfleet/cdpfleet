// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_DEDICATED_API_KEY (keep_alive needs dedicated threads), PROXY_URL
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_DEDICATED_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
const string Starter = "https://starter.cdpfleet.com";
const string Api = "https://cdpfleet.com";
const int IdleS = 30; // inactivity_timeout: how long the session waits for a client
const string Site = "https://books.toscrape.com";
var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var headers = new Dictionary<string, string> { ["x-api-key"] = key };

async Task<JsonObject> ApiGet(string path) => JsonNode.Parse(await http.GetStringAsync($"{Api}{path}"))!.AsObject();
// The session as the account API sees it while it runs: listed in /v1/me/threads or not.
async Task<JsonObject?> Listed(string sid) =>
    (await ApiGet("/v1/me/threads"))["threads"]!.AsArray().Select(t => t!.AsObject()).FirstOrDefault(t => (string?)t["session_id"] == sid);
// Today's thread-seconds on the account, closed and still running.
async Task<(int Closed, int Live)> Usage()
{
    var u = await ApiGet("/v1/me/usage");
    return ((int)u["closed_thread_seconds"]!, (int)u["live_thread_seconds"]!);
}
// A shared proxy drops the odd connection: one retry.
async Task Go(IPage page, string url)
{
    try { await page.GotoAsync(url, new() { Timeout = 45000 }); }
    catch (PlaywrightException) { await page.GotoAsync(url, new() { Timeout = 45000 }); }
}
double Secs(DateTime a, DateTime b) => Math.Round((b - a).TotalSeconds, 1);

var closedBefore = (await Usage()).Closed;

// 1. Launch with keep_alive: a disconnect no longer ends the session.
var t0 = DateTime.UtcNow;
using var res = await http.PostAsJsonAsync($"{Starter}/chromium/session",
    new { proxy, headless = "new", keep_alive = true, inactivity_timeout = $"{IdleS}s" });
var resBody = await res.Content.ReadAsStringAsync();
if (!res.IsSuccessStatusCode) throw new Exception($"launch failed: {(int)res.StatusCode} {resBody}");
var session = JsonNode.Parse(resBody)!.AsObject();
var sid = (string)session["sessionId"]!;
var wsUrl = (string)session["wsUrl"]!;

using var pw = await Playwright.CreateAsync();
var opts = new BrowserTypeConnectOptions { Headers = headers };

// 2. First client: an agent works through the catalogue in one context, a second context on the side.
var browser = await pw.Chromium.ConnectAsync(wsUrl, opts);
var work = await browser.NewContextAsync();
var page = await work.NewPageAsync();
await Go(page, $"{Site}/catalogue/page-1.html");
await page.ClickAsync("li.next a"); // step 2 of the run: page 2
await work.AddCookiesAsync(new[] { new Cookie { Name = "agent_step", Value = "2", Domain = "books.toscrape.com", Path = "/" } });
await page.EvaluateAsync("() => localStorage.setItem('agent_state', JSON.stringify({ seen: 40, next: 'page-3' }))");
await Go(await work.NewPageAsync(), $"{Site}/catalogue/a-light-in-the-attic_1000/index.html");
var side = await browser.NewContextAsync();
await Go(await side.NewPageAsync(), $"{Site}/catalogue/category/books/poetry_23/index.html");
var before = new JsonObject { ["contexts"] = browser.Contexts.Count, ["pages"] = browser.Contexts.Sum(c => c.Pages.Count), ["url"] = page.Url };

// 3. The client goes away. With keep_alive, CloseAsync() only disconnects.
await browser.CloseAsync();
var tDetach = DateTime.UtcNow;
await Task.Delay(3000);
var whileDetached = await Listed(sid);
await Task.Delay(12000);

// 4. A new client reconnects to the same wsUrl and carries on where the first one stopped.
var tReconnect = DateTime.UtcNow;
browser = await pw.Chromium.ConnectAsync(wsUrl, opts);
var reconnectMs = (long)(DateTime.UtcNow - tReconnect).TotalMilliseconds;
var detachedS = Secs(tDetach, tReconnect);
var contexts = browser.Contexts;
var all = contexts.SelectMany(c => c.Pages.Select(p => (c, p))).ToList();
var back = all.FirstOrDefault(x => x.p.Url.Contains("page-2.html"));
var urls = all.Select(x => x.p.Url.Replace(Site, "")).OrderBy(u => u, StringComparer.Ordinal).ToList();
string? cookie = null, local = null, continued = null;
if (back.p != null)
{
    cookie = (await back.c.CookiesAsync(new[] { Site })).FirstOrDefault(c => c.Name == "agent_step")?.Value;
    local = await back.p.EvaluateAsync<string?>("() => localStorage.getItem('agent_state')");
    await back.p.ClickAsync("li.next a");
    await back.p.WaitForURLAsync("**/page-3.html");
    continued = back.p.Url.Replace(Site, "");
}
var reconnect = new JsonObject
{
    ["ms"] = reconnectMs,
    ["contexts"] = contexts.Count,
    ["pages"] = all.Count,
    ["urls"] = new JsonArray(urls.Select(u => (JsonNode)u).ToArray()),
    ["cookie"] = cookie,
    ["local_storage"] = local,
    ["continued"] = continued,
};

// 5. The client leaves for good: the session waits inactivity_timeout, then ends.
await browser.CloseAsync();
var tLeave = DateTime.UtcNow;
double? goneAfter = null;
int? liveWaiting = null;
while ((DateTime.UtcNow - tLeave).TotalSeconds < IdleS + 90)
{
    await Task.Delay(2000);
    if (liveWaiting == null && (DateTime.UtcNow - tLeave).TotalSeconds >= 20) liveWaiting = (await Usage()).Live;
    if (await Listed(sid) == null) { goneAfter = Secs(tLeave, DateTime.UtcNow); break; }
}
JsonObject late;
try
{
    var b = await pw.Chromium.ConnectAsync(wsUrl, new BrowserTypeConnectOptions { Headers = headers, Timeout = 20000 });
    late = new JsonObject { ["connected"] = true, ["contexts"] = b.Contexts.Count };
    await b.CloseAsync();
}
catch (PlaywrightException e)
{
    var msg = e.Message.Split('\n')[0];
    late = new JsonObject { ["connected"] = false, ["error"] = msg[..Math.Min(160, msg.Length)] };
}

// 6. What was billed: the session's record in the account history.
await Task.Delay(5000);
var s = (await ApiGet("/v1/me/sessions?limit=20"))["sessions"]!.AsArray().Select(x => x!.AsObject()).FirstOrDefault(x => (string?)x["session_id"] == sid) ?? new JsonObject();
var closedAfter = (await Usage()).Closed;

var output = new JsonObject
{
    ["language"] = "csharp",
    ["inactivity_timeout_s"] = IdleS,
    ["before"] = before,
    ["detached"] = new JsonObject { ["listed"] = whileDetached != null, ["running_seconds"] = whileDetached?["running_seconds"]?.DeepClone(), ["seconds"] = detachedS },
    ["reconnect"] = reconnect,
    ["end"] = new JsonObject
    {
        ["gone_after_s"] = goneAfter,
        ["late_reconnect"] = late,
        ["end_reason"] = s["end_reason"]?.DeepClone(),
        ["duration_seconds"] = s["duration_seconds"]?.DeepClone(),
        ["weight"] = s["weight"]?.DeepClone(),
        ["client_seconds"] = Secs(t0, tLeave),
    },
    ["usage"] = new JsonObject { ["live_while_waiting"] = liveWaiting, ["billed"] = closedAfter - closedBefore },
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true }));
