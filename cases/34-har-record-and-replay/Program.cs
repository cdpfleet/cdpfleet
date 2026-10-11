// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Url = "https://books.toscrape.com/";

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
var dir = Directory.CreateTempSubdirectory("har-").FullName;
var har = Path.Combine(dir, "books.har");
try
{
    // 1. Record: every request of the page goes into a HAR file on this machine.
    var rec = await browser.NewContextAsync(new() { RecordHarPath = har, RecordHarMode = HarMode.Minimal });
    var p1 = await rec.NewPageAsync();
    var sw = System.Diagnostics.Stopwatch.StartNew();
    await p1.GotoAsync(Url, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 });
    var liveMs = sw.ElapsedMilliseconds;
    var liveBooks = await p1.Locator("article.product_pod").CountAsync();
    await rec.CloseAsync(); // writes the HAR
    var entries = JsonNode.Parse(await File.ReadAllTextAsync(har))!["log"]!["entries"]!.AsArray();

    // 2. Replay: serve every request from the HAR; anything not in it is refused, so the
    //    page can't quietly reach the network.
    var rep = await browser.NewContextAsync();
    var fromHar = 0;
    var refused = 0;
    rep.RequestFinished += (_, _) => fromHar++;
    rep.RequestFailed += (_, _) => refused++;
    await rep.RouteFromHARAsync(har, new() { NotFound = HarNotFound.Abort });
    var p2 = await rep.NewPageAsync();
    sw.Restart();
    await p2.GotoAsync(Url, new() { WaitUntil = WaitUntilState.Load, Timeout = 60000 });
    var replayMs = sw.ElapsedMilliseconds;
    var replayBooks = await p2.Locator("article.product_pod").CountAsync();
    // A page the HAR doesn't have: refused instead of fetched.
    string missing;
    try
    {
        await p2.GotoAsync("https://books.toscrape.com/catalogue/page-2.html");
        missing = "loaded";
    }
    catch (PlaywrightException e)
    {
        missing = Regex.IsMatch(e.Message, "ERR_FAILED|aborted", RegexOptions.IgnoreCase) ? "refused" : e.Message.Split('\n')[0];
    }
    await rep.CloseAsync();

    var result = new JsonObject
    {
        ["har_entries"] = entries.Count,
        ["har_kb"] = (long)Math.Round(new FileInfo(har).Length / 1024.0, MidpointRounding.AwayFromZero),
        ["live"] = new JsonObject { ["ms"] = liveMs, ["books"] = liveBooks },
        ["replay"] = new JsonObject { ["ms"] = replayMs, ["books"] = replayBooks, ["served_from_har"] = fromHar, ["refused"] = refused },
        ["page_not_in_har"] = missing,
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
    Directory.Delete(dir, true);
}
