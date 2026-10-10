// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

// Content a plain document.querySelector can't see: a same-origin iframe, a cross-origin
// iframe, an open shadow root (with another one nested inside) and a closed shadow root.
const string Html = """
    <!doctype html><title>Hidden content</title>
    <h1>Main document</h1>
    <iframe id="same" srcdoc="<p id='inner'>same-origin iframe text</p>"></iframe>
    <iframe id="cross" src="https://httpbin.org/html"></iframe>
    <open-card></open-card>
    <closed-card></closed-card>
    <script>
    customElements.define('open-card', class extends HTMLElement {
      connectedCallback() {
        const root = this.attachShadow({ mode: 'open' });
        root.innerHTML = '<p class="msg">open shadow text</p><nested-badge></nested-badge>';
      }
    });
    customElements.define('nested-badge', class extends HTMLElement {
      connectedCallback() { this.attachShadow({ mode: 'open' }).innerHTML = '<span class="badge">nested shadow text</span>'; }
    });
    customElements.define('closed-card', class extends HTMLElement {
      connectedCallback() { this.attachShadow({ mode: 'closed' }).innerHTML = '<p class="secret">closed shadow text</p>'; }
    });
    </script>
    """;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync();
    await page.SetContentAsync(Html);
    await page.FrameLocator("#cross").Locator("h1").WaitForAsync(new() { Timeout = 60000 });

    async Task<string?> Qs(string sel) => await page.EvaluateAsync<string?>("(s) => document.querySelector(s)?.textContent ?? null", sel);
    async Task<string?> Pw(ILocator loc) => await loc.CountAsync() > 0 ? await loc.First.TextContentAsync() : null;
    JsonObject Row(string target, string? querySelector, string? playwright, string how) =>
        new() { ["target"] = target, ["querySelector"] = querySelector, ["playwright"] = playwright, ["how"] = how };

    var rows = new JsonArray
    {
        Row("same-origin iframe", await Qs("#inner"), await Pw(page.FrameLocator("#same").Locator("#inner")), "page.frameLocator('#same').locator('#inner')"),
        Row("cross-origin iframe", await Qs("h1 + div p"), await Pw(page.FrameLocator("#cross").Locator("h1")), "page.frameLocator('#cross').locator('h1')"),
        Row("open shadow root", await Qs(".msg"), await Pw(page.Locator(".msg")), "page.locator('.msg') — CSS pierces open shadow roots"),
        Row("nested open shadow root", await Qs(".badge"), await Pw(page.Locator(".badge")), "page.locator('.badge') — any depth"),
        Row("closed shadow root", await Qs(".secret"), await Pw(page.Locator(".secret")), "not reachable from page scripts or locators"),
    };
    // The cross-origin frame is a separate document: its URL and title come from the frame object.
    var cross = page.Frames.FirstOrDefault(f => f.Url.StartsWith("https://httpbin.org"));

    var result = new JsonObject
    {
        ["frames"] = page.Frames.Count,
        ["cross_origin_frame"] = new JsonObject { ["url"] = cross?.Url, ["title"] = cross != null ? await cross.TitleAsync() : null },
        ["rows"] = rows,
    };
    Console.WriteLine(result.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
