// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

const string Site = "https://quotes.toscrape.com";
const string Next = "li.next a"; // the scraper assumes every listing page has a "Next" link

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);

var output = new JsonObject { ["site"] = Site, ["scrape"] = new JsonObject(), ["recording"] = new JsonObject(), ["unrecorded"] = new JsonObject() };
var scrape = output["scrape"]!.AsObject();
var recording = output["recording"]!.AsObject();
var unrecorded = output["unrecorded"]!.AsObject();
string sessionId;
using (var playwright = await Playwright.CreateAsync())
{
    // A session launched with record: false, for comparison: it must never get a recording.
    var quiet = await Launch(playwright, false);
    try { await Go(quiet.Page, Site); }
    finally { await quiet.Browser.CloseAsync(); }
    unrecorded["session_id"] = quiet.Id;

    // The scrape: pages 8, 9, 10 — follow "Next" on each. Page 10 is the last one and has none.
    var s = await Launch(playwright, null);
    sessionId = s.Id;
    output["session_id"] = sessionId;
    var quotes = new List<string>();
    try
    {
        await Go(s.Page, $"{Site}/page/8/");
        for (var pageNo = 8; ; pageNo++)
        {
            quotes.AddRange(await s.Page.Locator(".quote .text").AllTextContentsAsync());
            scrape["last_page"] = pageNo;
            string? href;
            try
            {
                href = await s.Page.Locator(Next).GetAttributeAsync("href", new() { Timeout = 5000 }); // 5 s to find the link
            }
            catch (Exception e) when (e is PlaywrightException or TimeoutException)
            {
                scrape["failed_at_s"] = Since(s.Started);
                scrape["error"] = $"{(e.GetType().Name == "TimeoutException" ? "timeout" : e.GetType().Name)} waiting for {Next}";
                break;
            }
            await Go(s.Page, new Uri(new Uri(Site), href).ToString()); // page loads keep their own timeout and retries
        }
        scrape["quotes"] = quotes.Count;
        // What the video shows at that moment: the last page — a "Previous" link and no "Next".
        scrape["url_at_failure"] = s.Page.Url;
        scrape["next_links"] = await s.Page.Locator("li.next").CountAsync();
        scrape["previous_links"] = await s.Page.Locator("li.previous").CountAsync();
    }
    finally
    {
        await s.Browser.CloseAsync(); // ends the session; the node uploads the video afterwards
    }
    output["session_seconds"] = Since(s.Started);
}

// Find the recording, check it covers the failure, download it, delete it.
var t = DateTime.UtcNow;
JsonObject? rec = null;
while (rec is null && (DateTime.UtcNow - t).TotalSeconds < 600)
{
    rec = (await Recordings()).FirstOrDefault(r => (string?)r!["session_id"] == sessionId)?.AsObject();
    if (rec is null) await Task.Delay(5000);
}
if (rec is not null)
{
    recording["appeared_after_s"] = (long)Math.Round((DateTime.UtcNow - t).TotalSeconds);
    var id = (string)rec["id"]!;
    var link = (await http.GetFromJsonAsync<JsonObject>($"https://cdpfleet.com/v1/me/recordings/{id}/url"))!;
    using var plain = new HttpClient(); // the private link needs no API key, so none is sent
    using var video = await plain.GetAsync((string)link["url"]!);
    var bytes = await video.Content.ReadAsByteArrayAsync();
    foreach (var k in new[] { "duration_seconds", "width", "height", "bytes" }) recording[k] = rec[k]!.DeepClone();
    recording["downloaded_bytes"] = bytes.Length;
    recording["content_type"] = video.Content.Headers.ContentType?.MediaType;
    recording["mp4"] = bytes.Length > 8 && System.Text.Encoding.ASCII.GetString(bytes, 4, 4) == "ftyp";
    recording["link_expires_in_s"] = link["expires_in_s"]!.DeepClone();
    output["failure_within_recording"] = (double)scrape["failed_at_s"]! <= (double)rec["duration_seconds"]! + 1;
    recording["deleted"] = (await http.DeleteAsync($"https://cdpfleet.com/v1/me/recordings/{id}")).IsSuccessStatusCode;
}

// By now the scrape's video has been uploaded; give the unrecorded session another minute.
await Task.Delay(60000);
unrecorded["recording_found"] = (await Recordings()).Any(r => (string?)r!["session_id"] == (string)unrecorded["session_id"]!);
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

async Task<JsonArray> Recordings() =>
    (await http.GetFromJsonAsync<JsonObject>("https://cdpfleet.com/v1/me/recordings"))?["recordings"]?.AsArray() ?? [];

// Headful (the default) sessions are recorded from the browser's display.
async Task<Session> Launch(IPlaywright playwright, bool? record)
{
    var body = new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = false };
    if (record is not null) body["record"] = record;
    var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session", body);
    if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
    var launched = (await res.Content.ReadFromJsonAsync<JsonObject>())!;
    var started = DateTime.UtcNow;
    var browser = await playwright.Chromium.ConnectAsync((string)launched["wsUrl"]!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    // NoViewport: the page fills the whole display, so it fills the recording too.
    var page = await (await browser.NewContextAsync(new() { ViewportSize = ViewportSize.NoViewport })).NewPageAsync();
    return new Session((string)launched["sessionId"]!, started, browser, page);
}

static double Since(DateTime started) => Math.Round((DateTime.UtcNow - started).TotalSeconds, 1);

// Proxies drop a tunnel now and then: up to two retries.
static async Task Go(IPage page, string url)
{
    for (var attempt = 1; ; attempt++)
    {
        try { await page.GotoAsync(url, new() { Timeout = 60000 }); return; }
        catch (Exception e) when (attempt < 3 && e is PlaywrightException or TimeoutException) { await Task.Delay(2000); }
    }
}

record Session(string Id, DateTime Started, IBrowser Browser, IPage Page);
