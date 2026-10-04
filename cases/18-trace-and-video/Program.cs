// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.IO.Compression;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var dir = Directory.CreateTempSubdirectory("cdpfleet-replay-").FullName;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

var sw = Stopwatch.StartNew();
using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    // Video is recorded on the remote browser and fetched when the page closes; the trace is
    // assembled by the client from events and screenshots streamed over the same connection.
    var context = await browser.NewContextAsync(new()
    {
        RecordVideoDir = dir, RecordVideoSize = new() { Width = 1280, Height = 720 }, ViewportSize = new() { Width = 1280, Height = 720 },
    });
    await context.Tracing.StartAsync(new() { Screenshots = true, Snapshots = true });
    var page = await context.NewPageAsync();
    var video = page.Video!; // take the handle while the page is open
    await page.GotoAsync("https://example.com/", new() { Timeout = 60000 });
    await page.GotoAsync("https://httpbin.org/forms/post", new() { Timeout = 60000 });
    await page.GetByLabel("Customer name").FillAsync("Ada Lovelace");
    await page.GetByLabel("Large").CheckAsync();
    await page.GetByRole(AriaRole.Button, new() { Name = "Submit order" }).ClickAsync();
    await page.WaitForURLAsync("**/post", new() { Timeout = 60000 }); // httpbin echoes the form as JSON
    await page.ScreenshotAsync(new() { Path = Path.Combine(dir, "final.png") });
    await context.Tracing.StopAsync(new() { Path = Path.Combine(dir, "trace.zip") });
    await context.CloseAsync(); // finishes the video
    await video.SaveAsAsync(Path.Combine(dir, "session.webm"));
}
finally
{
    await browser.CloseAsync();
}
var seconds = sw.Elapsed.TotalSeconds;

var actions = new List<string>();
int screenshots = 0, snapshots = 0, network = 0;
using (var zip = ZipFile.OpenRead(Path.Combine(dir, "trace.zip")))
{
    foreach (var entry in zip.Entries)
    {
        var name = entry.FullName;
        if (name.StartsWith("resources/") && name.EndsWith(".jpeg")) screenshots++;
        if (!name.EndsWith(".trace") && !name.EndsWith(".network")) continue;
        using var reader = new StreamReader(entry.Open());
        foreach (var line in (await reader.ReadToEndAsync()).Split('\n'))
        {
            if (string.IsNullOrWhiteSpace(line)) continue;
            if (name.EndsWith(".network")) { network++; continue; }
            var ev = JsonNode.Parse(line)!.AsObject();
            var type = (string?)ev["type"] ?? "";
            if (type == "frame-snapshot") snapshots++;
            if (type == "before" && ev["method"] is JsonNode m) actions.Add((string)m!);
        }
    }
}
var webm = await File.ReadAllBytesAsync(Path.Combine(dir, "session.webm"));
var isWebm = webm.Length >= 4 && webm[0] == 0x1a && webm[1] == 0x45 && webm[2] == 0xdf && webm[3] == 0xa3;

JsonObject Row(string artifact, long? bytes, string detail, int? shots, int? snaps, int? net, string? openWith) => new()
{
    ["artifact"] = artifact, ["bytes"] = bytes, ["detail"] = detail, ["screenshots"] = shots, ["snapshots"] = snaps,
    ["network_entries"] = net, ["open_with"] = openWith,
};
var result = new JsonArray
{
    Row("trace.zip", new FileInfo(Path.Combine(dir, "trace.zip")).Length, $"{actions.Count} actions: {string.Join(", ", actions)}",
        screenshots, snapshots, network, "npx playwright show-trace trace.zip"),
    Row("session.webm", webm.Length, isWebm ? "valid WebM (EBML header)" : "not a WebM file", null, null, null, "any video player"),
    Row("final.png", new FileInfo(Path.Combine(dir, "final.png")).Length, "screenshot after the last step", null, null, null, "image viewer"),
    Row("whole run", null, $"{seconds.ToString("F1", System.Globalization.CultureInfo.InvariantCulture)} s including recording and downloads", null, null, null, null),
};
Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
