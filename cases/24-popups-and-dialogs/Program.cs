// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// A page with everything that interrupts a script: a new-tab link, window.open, and the
// three blocking dialogs. Served from the browser itself, so the case needs no third party.
const string Html = @"<!doctype html><title>Interruptions</title>
<a id=""blank"" href=""https://example.com/"" target=""_blank"">open in a new tab</a>
<button id=""open"" onclick=""window.open('https://example.com/?popup', 'pop', 'width=480,height=320')"">window.open</button>
<button id=""alert"" onclick=""alert('Saved!')"">alert</button>
<button id=""confirm"" onclick=""document.body.dataset.confirm = String(confirm('Delete 3 items?'))"">confirm</button>
<button id=""prompt"" onclick=""document.body.dataset.prompt = String(prompt('Your name?', 'anonymous'))"">prompt</button>";

static JsonObject Row(string ev, string what, string handledWith, int pagesOpen, bool? opener) => new()
{
    ["event"] = ev,
    ["what_happened"] = what,
    ["handled_with"] = handledWith,
    ["pages_open"] = pagesOpen,
    ["opener_is_main_page"] = opener is null ? null : JsonValue.Create(opener.Value),
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL"), headless = "new" });
if (!res.IsSuccessStatusCode) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var session = await res.Content.ReadFromJsonAsync<JsonElement>();

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(session.GetProperty("wsUrl").GetString()!,
    new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    await page.SetContentAsync(Html);
    var output = new JsonArray();

    // New pages: listen on the context BEFORE the click, then wait for the popup to load.
    foreach (var (label, selector) in new[] { ("link with target=_blank", "#blank"), ("window.open()", "#open") })
    {
        var popup = await context.RunAndWaitForPageAsync(async () => await page.ClickAsync(selector));
        await popup.WaitForLoadStateAsync(LoadState.Load, new() { Timeout = 60000 });
        output.Add(Row(label, $"new page: {popup.Url} — \"{await popup.TitleAsync()}\"",
            "context.waitForEvent(\"page\") + popup.waitForLoadState()", context.Pages.Count, await popup.OpenerAsync() == page));
        await popup.CloseAsync();
    }

    // Dialogs: without a handler Playwright dismisses them (confirm → false, prompt → null).
    await page.ClickAsync("#confirm");
    output.Add(Row("confirm() with no dialog handler", $"page saw confirm() return {await page.EvaluateAsync<string>("() => document.body.dataset.confirm")}",
        "nothing — auto-dismissed", context.Pages.Count, null));

    // With a handler you decide: accept, dismiss, or type an answer.
    var seen = new List<string>();
    page.Dialog += async (_, d) =>
    {
        seen.Add($"{d.Type}: \"{d.Message}\"");
        if (d.Type == "prompt") await d.AcceptAsync("Ada Lovelace"); else await d.AcceptAsync();
    };
    await page.ClickAsync("#alert");
    await page.ClickAsync("#confirm");
    await page.ClickAsync("#prompt");
    var results = await page.EvaluateAsync<JsonElement>("() => ({ confirm: document.body.dataset.confirm, prompt: document.body.dataset.prompt })");
    output.Add(Row("alert()", seen[0], "dialog.accept()", context.Pages.Count, null));
    output.Add(Row("confirm() with a handler", $"{seen[1]} → page saw {results.GetProperty("confirm").GetString()}", "dialog.accept()", context.Pages.Count, null));
    output.Add(Row("prompt()", $"{seen[2]} → page saw \"{results.GetProperty("prompt").GetString()}\"", "dialog.accept(\"Ada Lovelace\")", context.Pages.Count, null));

    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
