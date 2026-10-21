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
const int WaitSeconds = 120; // between the two snapshots — in a real watcher, your scheduler's interval

// What to watch: a catalogue page (its price only changes when the shop changes it) and a live
// market price that moves by the second.
var targets = new (string Name, string Url, string Read)[]
{
    ("Book price", "https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html",
        @"() => ({ price: document.querySelector('.product_main .price_color').textContent.trim(), stock: document.querySelector('.product_main .availability').textContent.trim().replace(/\s+/g, ' ') })"),
    ("Bitcoin spot (USD)", "https://api.coinbase.com/v2/prices/BTC-USD/spot", "() => ({ price: JSON.parse(document.body.innerText).data.amount })"),
};

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync("https://starter.cdpfleet.com/chromium/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// One snapshot = one short session: launch, read every target, close. Nothing is billed while
// waiting, and an idle session would end after 2 minutes (the default) anyway.
async Task<(string At, double Seconds, Dictionary<string, JsonObject> Values)> Snapshot()
{
    var t = DateTime.UtcNow;
    var s = await Launch(new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = true, ["record"] = false });
    var browser = await playwright.Chromium.ConnectAsync(s.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    try
    {
        var page = await browser.NewPageAsync();
        var values = new Dictionary<string, JsonObject>();
        foreach (var target in targets)
        {
            for (var attempt = 1; ; attempt++) // proxies drop a tunnel now and then
            {
                try
                {
                    await page.GotoAsync(target.Url, new() { Timeout = 45000 });
                    values[target.Name] = JsonNode.Parse((await page.EvaluateAsync<JsonElement>(target.Read)).GetRawText())!.AsObject();
                    break;
                }
                catch (PlaywrightException) when (attempt < 3) { }
            }
        }
        return (DateTime.UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fffZ"), Math.Round((DateTime.UtcNow - t).TotalSeconds, 1), values);
    }
    finally
    {
        await browser.CloseAsync();
    }
}

double Number(string v) => double.TryParse(Regex.Replace(v, "[^0-9.]", ""), System.Globalization.NumberStyles.Float, System.Globalization.CultureInfo.InvariantCulture, out var n) ? n : 0;

var first = await Snapshot();
await Task.Delay(WaitSeconds * 1000);
var second = await Snapshot();

// Field-by-field diff of two snapshots, with the percentage for numeric prices.
var diff = new JsonArray();
foreach (var target in targets)
{
    var changes = new JsonArray();
    foreach (var (k, v) in first.Values[target.Name])
    {
        string x = v!.GetValue<string>(), y = second.Values[target.Name][k]!.GetValue<string>();
        if (x == y) continue;
        var c = new JsonObject { ["field"] = k, ["before"] = x, ["after"] = y };
        double nx = Number(x), ny = Number(y);
        if (nx != 0 && ny != 0) c["percent"] = Math.Round((ny - nx) / nx * 100, 2);
        changes.Add(c);
    }
    diff.Add(new JsonObject { ["target"] = target.Name, ["changed"] = changes.Count > 0, ["changes"] = changes, ["now"] = second.Values[target.Name].DeepClone() });
}
Console.WriteLine(new JsonObject
{
    ["wait_seconds"] = WaitSeconds,
    ["billed_seconds"] = Math.Round(first.Seconds + second.Seconds, 1),
    ["first_at"] = first.At,
    ["second_at"] = second.At,
    ["diff"] = diff,
}.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
