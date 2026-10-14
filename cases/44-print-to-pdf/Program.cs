// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// Which browsers can print a page to PDF over a remote connection, headless and headful.
var setups = new (string Label, string Engine, string Family, JsonNode Headless)[]
{
    ("Chromium, headless", "chromium", "chromium", JsonValue.Create(true)),
    ("Chrome, headless", "chrome", "chromium", JsonValue.Create("new")),
    ("Chrome, headful", "chrome", "chromium", JsonValue.Create(false)),
    ("Edge, headful", "edge", "chromium", JsonValue.Create(false)),
    ("Firefox, headful", "firefox", "firefox", JsonValue.Create(false)),
    ("Camoufox, headful", "camoufox", "firefox", JsonValue.Create(false)),
    ("WebKit, headful", "webkit", "webkit", JsonValue.Create(false)),
};

// A page with a coloured background, a screen-only banner hidden by print CSS and enough text
// for several printed pages. SetContent: no proxy traffic, the same page everywhere.
var paras = string.Concat(Enumerable.Range(1, 120).Select(i => $"<p>Paragraph {i}: lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>"));
var html = "<html><head><style>"
    + "body { font: 14px sans-serif; background: #e8f0ff; }"
    + ".screen-only { background: #c00; color: #fff; padding: 8px; }"
    + "@media print { .screen-only { display: none; } }"
    + "</style></head><body><div class=\"screen-only\">Cookie banner (screen only)</div><h1>Quarterly report</h1>"
    + paras + "</body></html>";

int PagesIn(byte[] pdf) => Regex.Matches(Encoding.Latin1.GetString(pdf), @"/Type\s*/Page(?!s)").Count;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {engine} {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

async Task<JsonObject> One(string label, string engine, string family, JsonNode headless)
{
    var session = await Launch(engine, new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = headless.DeepClone() });
    var type = family switch { "firefox" => playwright.Firefox, "webkit" => playwright.Webkit, _ => playwright.Chromium };
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var outp = new JsonObject { ["label"] = label };
    try
    {
        var page = await browser.NewPageAsync();
        await page.SetContentAsync(html);
        var t = DateTime.UtcNow;
        try
        {
            var pdf = await page.PdfAsync(new() { Format = "A4" });
            var withBg = await page.PdfAsync(new() { Format = "A4", PrintBackground = true });
            outp["pdf"] = "yes";
            outp["pdf_bytes"] = pdf.Length;
            outp["pdf_pages"] = PagesIn(pdf);
            outp["with_background_bytes"] = withBg.Length;
            outp["pdf_seconds"] = Math.Round((DateTime.UtcNow - t).TotalSeconds, 2);
        }
        catch (PlaywrightException err) // Firefox, Camoufox and WebKit refuse
        {
            var msg = err.Message.Split('\n')[0];
            outp["pdf"] = "no";
            outp["error"] = msg[..Math.Min(120, msg.Length)];
        }
        // The fallback that works everywhere: print CSS + a full-page screenshot.
        await page.EmulateMediaAsync(new() { Media = Media.Print });
        outp["print_css_applied"] = await page.EvaluateAsync<bool>("() => getComputedStyle(document.querySelector('.screen-only')).display === 'none'");
        outp["print_screenshot_kb"] = (int)Math.Round((await page.ScreenshotAsync(new() { FullPage = true })).Length / 1024.0);
    }
    finally
    {
        await browser.CloseAsync();
    }
    return outp;
}

var results = new JsonArray();
foreach (var s in setups) results.Add(await One(s.Label, s.Engine, s.Family, s.Headless));
Console.WriteLine(new JsonObject { ["results"] = results }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
