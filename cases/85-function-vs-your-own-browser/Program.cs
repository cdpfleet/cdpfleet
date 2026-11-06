// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Buffers.Binary;
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!; // the same proxy for the Function calls and the sessions
const string Api = "https://cdpfleet.com";
const string Starter = "https://starter.cdpfleet.com";
var pages = new[] {
    ("example.com", "https://example.com/"),
    ("books.toscrape.com", "https://books.toscrape.com/"),
    ("wikipedia", "https://en.wikipedia.org/wiki/Web_scraping"),
};
var tasks = new[] { "screenshot", "pdf", "markdown" };
var since = DateTime.UtcNow.AddMinutes(-1).ToString("o");
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(90) };
http.DefaultRequestHeaders.Add("x-api-key", key);
var headers = new Dictionary<string, string> { ["x-api-key"] = key };

// The record of a session or a Function call in the account's history (a call's x-call-id is its
// session id there). Billed thread-seconds = (ended_at - started_at) × weight.
async Task<JsonObject?> Billed(string id)
{
    for (var i = 0; i < 40; i++)
    {
        var r = JsonNode.Parse(await http.GetStringAsync($"{Api}/v1/me/sessions?limit=200&from={Uri.EscapeDataString(since)}"))!;
        var s = r["sessions"]!.AsArray().Select(x => x!.AsObject()).FirstOrDefault(x => (string?)x["session_id"] == id);
        if (s != null && s["ended_at"] != null)
        {
            var ms = (DateTimeOffset.Parse((string)s["ended_at"]!) - DateTimeOffset.Parse((string)s["started_at"]!)).TotalMilliseconds;
            var w = (int)s["weight"]!;
            return new JsonObject { ["engine"] = (string?)s["engine"], ["weight"] = w, ["thread_seconds"] = Math.Round(ms * w / 1000, 1) };
        }
        await Task.Delay(2000);
    }
    return null;
}

// What a file is: PNG width × height; for a PDF, whether it has fonts (a text PDF), its page objects
// (0 when they sit in compressed object streams) and the tallest image in it.
void Describe(string task, byte[] buf, JsonObject o)
{
    if (task == "screenshot")
    {
        o["width"] = BinaryPrimitives.ReadInt32BigEndian(buf.AsSpan(16, 4));
        o["height"] = BinaryPrimitives.ReadInt32BigEndian(buf.AsSpan(20, 4));
    }
    else if (task == "pdf")
    {
        var s = Encoding.Latin1.GetString(buf);
        var heights = Regex.Matches(s, @"/Height (\d+)").Select(m => int.Parse(m.Groups[1].Value)).ToList();
        o["text_pdf"] = Regex.IsMatch(s, @"/Font\b");
        o["pages"] = Regex.Matches(s, @"/Type\s*/Page(?![s\w])").Count;
        o["image_height"] = heights.Count > 0 ? heights.Max() : 0;
    }
}

// (a) The Function: one POST, the file in the response. One retry if the call fails.
async Task<JsonObject> ViaFunction(string task, string url)
{
    var body = new JsonObject { ["url"] = url, ["proxy"] = proxy };
    if (task == "screenshot") body["full_page"] = true;
    if (task == "pdf") body["format"] = "A4";
    var sw = Stopwatch.StartNew();
    var calls = new List<string>();
    HttpResponseMessage res = null!;
    byte[] buf = [];
    for (var attempt = 1; attempt <= 2; attempt++)
    {
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(40));
        res = await http.PostAsync($"{Api}/v1/functions/{task}", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"), cts.Token);
        buf = await res.Content.ReadAsByteArrayAsync(cts.Token);
        if (res.Headers.TryGetValues("x-call-id", out var ids)) calls.Add(ids.First());
        if ((int)res.StatusCode == 200) break;
    }
    var ms = sw.ElapsedMilliseconds;
    var records = new List<JsonObject?>();
    foreach (var id in calls) records.Add(await Billed(id));
    var last = records.LastOrDefault();
    var o = new JsonObject
    {
        ["status"] = (int)res.StatusCode, ["attempts"] = calls.Count, ["ms"] = ms, ["bytes"] = buf.Length,
        ["content_type"] = res.Content.Headers.ContentType?.ToString(),
        ["partial"] = res.Headers.TryGetValues("x-cdpfleet-partial", out var p) ? p.First() : null,
        ["engine"] = last?["engine"]?.DeepClone(),
        ["weight"] = last?["weight"]?.DeepClone(),
        ["thread_seconds"] = records.All(r => r != null) ? (JsonNode?)Math.Round(records.Sum(r => (double)r!["thread_seconds"]!), 1) : null,
    };
    if ((int)res.StatusCode == 200)
    {
        Describe(task, buf, o);
        if (task == "markdown")
        {
            var md = Encoding.UTF8.GetString(buf);
            var first = md.Split('\n')[0];
            o["first_line"] = first[..Math.Min(80, first.Length)];
            o["has_source_line"] = md.Contains("\nSource: ");
        }
    }
    return o;
}

using var pw = await Playwright.CreateAsync();

// (b) Your own session: launch over HTTP, connect with the key header.
async Task<(string Id, IBrowser Browser)> Launch()
{
    using var res = await http.PostAsJsonAsync($"{Starter}/chromium/session", new { proxy, headless = true });
    var text = await res.Content.ReadAsStringAsync();
    if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {text}");
    var s = JsonNode.Parse(text)!;
    return ((string)s["sessionId"]!, await pw.Chromium.ConnectAsync((string)s["wsUrl"]!, new() { Headers = headers }));
}

async Task<IPage> Open(IBrowser browser, string url)
{
    var page = await browser.NewPageAsync(new() { ViewportSize = new() { Width = 1280, Height = 720 } });
    // A proxy drops the odd connection: one retry.
    try { await page.GotoAsync(url, new() { Timeout = 45000 }); }
    catch (PlaywrightException) { await page.GotoAsync(url, new() { Timeout = 45000 }); }
    return page;
}

// Markdown has no Playwright call: you'd add a converter (ReverseMarkdown, …). Measured here: the HTML
// you would convert and the page's innerText.
async Task<JsonObject> Capture(IPage page, string task)
{
    var o = new JsonObject();
    if (task == "screenshot" || task == "pdf")
    {
        var buf = task == "screenshot" ? await page.ScreenshotAsync(new() { FullPage = true }) : await page.PdfAsync(new() { Format = "A4" });
        o["bytes"] = buf.Length;
        Describe(task, buf, o);
        return o;
    }
    var html = Encoding.UTF8.GetByteCount(await page.ContentAsync());
    var txt = Encoding.UTF8.GetByteCount(await page.Locator("body").InnerTextAsync());
    o["bytes"] = html;
    o["html_bytes"] = html;
    o["text_bytes"] = txt;
    return o;
}

async Task<JsonObject> ViaSession(string task, string url)
{
    var sw = Stopwatch.StartNew();
    var (id, browser) = await Launch();
    var launchMs = sw.ElapsedMilliseconds;
    JsonObject cap;
    try { cap = await Capture(await Open(browser, url), task); }
    finally { await browser.CloseAsync(); } // closing the connection ends the session
    var ms = sw.ElapsedMilliseconds;
    var rec = await Billed(id);
    var o = new JsonObject
    {
        ["ms"] = ms, ["launch_ms"] = launchMs,
        ["engine"] = rec?["engine"]?.DeepClone(), ["weight"] = rec?["weight"]?.DeepClone(), ["thread_seconds"] = rec?["thread_seconds"]?.DeepClone(),
    };
    foreach (var (k, v) in cap) o[k] = v?.DeepClone();
    return o;
}

var runs = new JsonArray();
foreach (var (name, url) in pages)
    foreach (var task in tasks)
        runs.Add(new JsonObject { ["page"] = name, ["task"] = task, ["function"] = await ViaFunction(task, url), ["session"] = await ViaSession(task, url) });

// (c) One session for all nine captures: launch once, three pages, three outputs each.
var oneSw = Stopwatch.StartNew();
var (oneId, oneBrowser) = await Launch();
var captures = 0;
try
{
    foreach (var (_, url) in pages)
    {
        var page = await Open(oneBrowser, url);
        foreach (var task in tasks) { await Capture(page, task); captures++; }
        await page.CloseAsync();
    }
}
finally { await oneBrowser.CloseAsync(); }
var oneMs = oneSw.ElapsedMilliseconds;
var one = await Billed(oneId);

Console.WriteLine(new JsonObject
{
    ["language"] = "csharp",
    ["proxy"] = "the same PROXY_URL for every Function call and every session",
    ["runs"] = runs,
    ["one_session"] = new JsonObject { ["captures"] = captures, ["ms"] = oneMs, ["thread_seconds"] = one?["thread_seconds"]?.DeepClone(), ["weight"] = one?["weight"]?.DeepClone() },
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
}.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
