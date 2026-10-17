// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL (your own proxy; a few KB of traffic)
using System.Net.Http.Json;
using System.Net.WebSockets;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
var headers = new Dictionary<string, string> { ["x-api-key"] = key };

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session", new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

// The account API's session history: how long each session ran and why it ended.
async Task<JsonNode?> History(string sessionId)
{
    var since = Uri.EscapeDataString(DateTime.UtcNow.AddHours(-1).ToString("yyyy-MM-ddTHH:mm:ssZ"));
    for (var i = 0; i < 30; i++)
    {
        var rows = (await http.GetFromJsonAsync<JsonObject>($"https://cdpfleet.com/v1/me/sessions?limit=50&from={since}"))!["sessions"]!.AsArray();
        var row = rows.FirstOrDefault(r => (string?)r!["session_id"] == sessionId);
        if (row?["ended_at"] is JsonNode ended)
        {
            var seconds = (DateTimeOffset.Parse((string)ended!) - DateTimeOffset.Parse((string)row["started_at"]!)).TotalSeconds;
            return new JsonObject { ["seconds"] = (long)Math.Round(seconds), ["end_reason"] = (string?)row["end_reason"] };
        }
        await Task.Delay(2000);
    }
    return null;
}

// 1. Launch with a start page and no client: the browser opens it by itself. A twin session
//    with the same options gets no viewer at all, to compare with.
JsonObject Body() => new() { ["proxy"] = proxy, ["headless"] = true, ["live_view"] = true, ["record"] = false, ["start_url"] = "https://httpbin.org/", ["inactivity_timeout"] = "30s" };
var session = await Launch("chromium", Body());
var twin = await Launch("chromium", Body());
var wsUrl = session.GetProperty("wsUrl").GetString()!;

// 2. A live link in control mode — what "Launch interactive" in the dashboard opens for you.
var linkRes = await http.PostAsync($"https://{new Uri(wsUrl).Host}/admin/session/live",
    new StringContent(new JsonObject { ["session_id"] = session.GetProperty("sessionId").GetString(), ["mode"] = "control" }.ToJsonString(), Encoding.UTF8, "application/json"));
var live = (await linkRes.Content.ReadFromJsonAsync<JsonObject>())!;

// The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
using var viewer = new ClientWebSocket();
await viewer.ConnectAsync(new Uri((string)live["live_url"]!), CancellationToken.None);
var seen = new List<string>();
var frames = 0;
var reader = Task.Run(async () =>
{
    var buffer = new byte[1 << 20];
    while (viewer.State == WebSocketState.Open)
    {
        using var ms = new MemoryStream();
        WebSocketReceiveResult r;
        try
        {
            do { r = await viewer.ReceiveAsync(buffer, CancellationToken.None); ms.Write(buffer, 0, r.Count); } while (!r.EndOfMessage);
        }
        catch (WebSocketException) { return; }
        if (r.MessageType == WebSocketMessageType.Binary) { frames++; continue; }
        if (r.MessageType != WebSocketMessageType.Text) return;
        var m = JsonNode.Parse(Encoding.UTF8.GetString(ms.ToArray()))!;
        if ((string?)m["type"] != "pages") continue;
        var current = (string?)m["current"];
        var url = m["pages"]!.AsArray().Where(p => (string?)p!["targetId"] == current).Select(p => (string?)p!["url"]).FirstOrDefault();
        lock (seen) { if (url != null && (seen.Count == 0 || seen[^1] != url)) seen.Add(url); }
    }
});
Task Send(JsonObject m) => viewer.SendAsync(Encoding.UTF8.GetBytes(m.ToJsonString()), WebSocketMessageType.Text, true, CancellationToken.None);
bool Seen(string part) { lock (seen) return seen.Any(u => u.Contains(part)); }
async Task<bool> WaitForUrl(string part) { for (var i = 0; i < 60 && !Seen(part); i++) await Task.Delay(500); return Seen(part); }
async Task Key(string k)
{
    await Send(new JsonObject { ["type"] = "key", ["action"] = "keyDown", ["key"] = k, ["code"] = k });
    await Send(new JsonObject { ["type"] = "key", ["action"] = "keyUp", ["key"] = k, ["code"] = k });
}

// 3. The person (here a scripted stand-in): goes to the form from the address bar, then fills
//    the first three fields from the keyboard — Tab to a field, type.
var startShown = await WaitForUrl("httpbin.org");
await Send(new JsonObject { ["type"] = "navigate", ["url"] = "https://httpbin.org/forms/post" });
var formShown = await WaitForUrl("/forms/post");
await Task.Delay(1500);
foreach (var text in new[] { "Ada Lovelace", "+44 20 7946 0000", "ada@example.com" })
{
    await Key("Tab");
    await Task.Delay(200);
    await Send(new JsonObject { ["type"] = "text", ["text"] = text });
    await Task.Delay(300);
}
// 4. Then only watches — no input for 45 s, past the 30 s inactivity_timeout.
await Task.Delay(45000);

// The unwatched twin has had no input and no viewer for as long: it should be gone.
var twinAlive = true;
try { await (await playwright.Chromium.ConnectAsync(twin.GetProperty("wsUrl").GetString()!, new() { Headers = headers, Timeout = 15000 })).CloseAsync(); }
catch (PlaywrightException) { twinAlive = false; }

// 5. Your code joins the same browser and finds the person's page in the default context.
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = headers });
var output = new JsonObject
{
    ["live_protocol"] = (string?)live["protocol"],
    ["start_page_shown"] = startShown,
    ["form_shown"] = formShown,
    ["frames_while_by_hand"] = frames,
    ["watched_session_alive_after_45s"] = browser.IsConnected,
    ["unwatched_twin_alive_after_45s"] = twinAlive,
};
try
{
    var pages = browser.Contexts[0].Pages;
    var page = pages.First(p => p.Url.Contains("/forms/post"));
    output["pages_found"] = new JsonArray(pages.Select(p => (JsonNode)p.Url!).ToArray());
    var typed = new JsonObject();
    foreach (var name in new[] { "custname", "custtel", "custemail" }) typed[name] = await page.InputValueAsync($"input[name={name}]");
    output["typed_by_hand"] = typed;
    // 6. The code finishes the job: the choices a script makes reliably, then submit.
    await page.CheckAsync("input[name=size][value=medium]");
    await page.CheckAsync("input[name=topping][value=cheese]");
    await page.FillAsync("input[name=delivery]", "19:30");
    await page.RunAndWaitForNavigationAsync(() => page.ClickAsync("form button"), new() { UrlString = "**/post" });
    output["submitted"] = JsonNode.Parse((await page.TextContentAsync("body"))!)!["form"]!.DeepClone();
    await Task.Delay(1500);
    lock (seen) output["viewer_saw"] = new JsonArray(seen.Select(u => (JsonNode)u!).ToArray());
}
finally
{
    try { await viewer.CloseAsync(WebSocketCloseStatus.NormalClosure, "", CancellationToken.None); } catch (Exception) { }
    await browser.CloseAsync();
}

// 7. The account API's session history.
output["watched_session"] = await History(session.GetProperty("sessionId").GetString()!);
output["unwatched_twin"] = await History(twin.GetProperty("sessionId").GetString()!);
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
