// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;

// Three visitors who must not see each other's state — in ONE browser session (1 thread).
var personas = new[]
{
    (Name: "alice", Locale: "en-US", Timezone: "America/New_York"),
    (Name: "bruno", Locale: "pt-BR", Timezone: "America/Sao_Paulo"),
    (Name: "chie", Locale: "ja-JP", Timezone: "Asia/Tokyo"),
};

const string Seen = @"() => ({
  cookie: document.cookie,
  storage_owner: localStorage.getItem('owner'),
  language: navigator.language,
  timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  clock: new Date('2026-10-05T12:00:00Z').toLocaleTimeString(),
})";

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
    var pages = new List<IPage>();
    foreach (var p in personas)
    {
        // Each context is a separate profile: its own cookies, storage, locale and clock.
        var context = await browser.NewContextAsync(new() { Locale = p.Locale, TimezoneId = p.Timezone });
        await context.AddCookiesAsync(new[] { new Cookie { Name = "session", Value = $"{p.Name}-token", Domain = "example.com", Path = "/" } });
        var page = await context.NewPageAsync();
        await page.GotoAsync("https://example.com/", new() { Timeout = 60000 });
        await page.EvaluateAsync("(n) => localStorage.setItem('owner', n)", p.Name);
        pages.Add(page);
    }
    var output = new JsonArray();
    for (var i = 0; i < personas.Length; i++)
    {
        var page = pages[i];
        // Read everything after all three exist, so any leak between them would show.
        var seen = await page.EvaluateAsync<JsonElement>(Seen);
        var ip = JsonNode.Parse(await (await page.GotoAsync("http://ip-api.com/json/?fields=query", new() { Timeout = 60000 }))!.TextAsync())!;
        var row = new JsonObject
        {
            ["persona"] = personas[i].Name,
            ["threads"] = session.GetProperty("weight").GetInt32(),
        };
        foreach (var k in new[] { "cookie", "storage_owner", "language", "timezone", "clock" })
            row[k] = JsonNode.Parse(seen.GetProperty(k).GetRawText());
        row["exit_ip"] = (string)ip["query"]!;
        output.Add(row);
    }
    Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
