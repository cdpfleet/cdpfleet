// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
string Sha256(byte[] b) => Convert.ToHexString(SHA256.HashData(b)).ToLowerInvariant();

// A local file to upload: 2,000 CSV rows (~60 KB).
var uploadPath = Path.Combine(Path.GetTempPath(), "cdpfleet-upload.csv");
var csv = new StringBuilder("id,token\n");
for (var i = 1; i <= 2000; i++) csv.Append($"{i},{Convert.ToHexString(RandomNumberGenerator.GetBytes(12)).ToLowerInvariant()}\n");
await File.WriteAllTextAsync(uploadPath, csv.ToString());
var local = await File.ReadAllBytesAsync(uploadPath);

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

// A small page on httpbin.org's origin with an upload form and a download link.
const string PageHtml = """
    <form method="post" action="/anything" enctype="multipart/form-data">
      <input type="file" name="upload" id="file"><button id="send">Send</button></form>
    <a id="data" href="/bytes/102400?seed=42" download="data.bin">data</a>
    """;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync();
    await page.RouteAsync("https://httpbin.org/files-demo", route => route.FulfillAsync(new() { ContentType = "text/html", Body = PageHtml }));
    await page.GotoAsync("https://httpbin.org/files-demo", new() { Timeout = 60000 });

    // Upload: SetInputFilesAsync reads the file HERE and streams it to the remote browser.
    var sw = Stopwatch.StartNew();
    await page.SetInputFilesAsync("#file", uploadPath);
    var answer = await page.RunAndWaitForNavigationAsync(() => page.ClickAsync("#send"), new() { Timeout = 60000 });
    var echoed = Encoding.UTF8.GetBytes((string)JsonNode.Parse(await answer!.TextAsync())!["files"]!["upload"]!);
    var uploadMs = sw.ElapsedMilliseconds;

    // Download: the file lands on the remote server; SaveAsAsync streams it back here.
    await page.GotoAsync("https://httpbin.org/files-demo", new() { Timeout = 60000 });
    sw.Restart();
    var download = await page.RunAndWaitForDownloadAsync(() => page.ClickAsync("#data"), new() { Timeout = 60000 });
    var downloadPath = Path.Combine(Path.GetTempPath(), download.SuggestedFilename);
    await download.SaveAsAsync(downloadPath);
    var downloadMs = sw.ElapsedMilliseconds;
    var got = await File.ReadAllBytesAsync(downloadPath);
    // The same seeded bytes fetched directly from here, to prove the copy is exact.
    var direct = await http.GetByteArrayAsync("https://httpbin.org/bytes/102400?seed=42");

    var result = new JsonObject
    {
        ["upload"] = new JsonObject
        {
            ["local_file"] = Path.GetFileName(uploadPath), ["bytes"] = local.Length, ["sha256"] = Sha256(local),
            ["server_received_bytes"] = echoed.Length, ["server_sha256"] = Sha256(echoed),
            ["identical"] = Sha256(local) == Sha256(echoed), ["ms"] = uploadMs,
        },
        ["download"] = new JsonObject
        {
            ["suggested_filename"] = download.SuggestedFilename, ["bytes"] = got.Length, ["sha256"] = Sha256(got),
            ["direct_sha256"] = Sha256(direct), ["identical"] = Sha256(got) == Sha256(direct), ["ms"] = downloadMs,
        },
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
