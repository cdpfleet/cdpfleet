// dotnet add package ModelContextProtocol --version 2.3.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Text.Json;
using System.Text.RegularExpressions;
using ModelContextProtocol.Client;
using ModelContextProtocol.Protocol;

var mcp = new Uri("https://cdpfleet.com/mcp");
var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
const string Extract = "() => [...document.querySelectorAll('article.product_pod')].map((a) => ({ title: a.querySelector('h3 a').title, price: a.querySelector('.price_color').textContent }))";

// One MCP session over Streamable HTTP; a script authenticates with its API key as a bearer token.
// ownsSession: end the MCP session (DELETE) when the client is disposed.
Task<McpClient> Connect(bool ownsSession) => McpClient.CreateAsync(new HttpClientTransport(new HttpClientTransportOptions
{
    Endpoint = mcp,
    TransportMode = HttpTransportMode.StreamableHttp,
    AdditionalHeaders = new Dictionary<string, string> { ["Authorization"] = $"Bearer {key}" },
    OwnsSession = ownsSession,
}));

async Task<(string Text, bool Error)> Call(McpClient client, List<Dictionary<string, object?>> log, string name, Dictionary<string, object?>? args = null)
{
    var sw = Stopwatch.StartNew();
    var r = await client.CallToolAsync(name, args ?? new Dictionary<string, object?>());
    var text = string.Join("\n", r.Content.OfType<TextContentBlock>().Select(c => c.Text));
    log.Add(new() { ["tool"] = name, ["ms"] = sw.ElapsedMilliseconds, ["error"] = r.IsError == true });
    return (text, r.IsError == true);
}

// A shared proxy drops the odd connection: one retry of a failed page load.
async Task<(string Text, bool Error)> Navigate(McpClient client, List<Dictionary<string, object?>> log, string url)
{
    var r = await Call(client, log, "navigate", new() { ["url"] = url });
    return r.Error ? await Call(client, log, "navigate", new() { ["url"] = url }) : r;
}

static string FirstLine(string s) => s.Split('\n')[0];
var launchArgs = new Dictionary<string, object?> { ["browser"] = "chromium", ["headless"] = true, ["proxy"] = proxy, ["record"] = false };
var sessionRe = new Regex("session ([0-9a-f-]{36})");

// ---- 1. One MCP session for the whole job, as Claude or Cursor keep one.
async Task<Dictionary<string, object?>> OneSession()
{
    var log = new List<Dictionary<string, object?>>();
    var o = new Dictionary<string, object?>();
    var sw = Stopwatch.StartNew();
    await using (var client = await Connect(ownsSession: true))
    {
        log.Add(new() { ["tool"] = "initialize", ["ms"] = sw.ElapsedMilliseconds, ["error"] = false });
        sw.Restart();
        var tools = await client.ListToolsAsync();
        log.Add(new() { ["tool"] = "tools/list", ["ms"] = sw.ElapsedMilliseconds, ["error"] = false });
        o["mcp_session"] = client.SessionId != null;
        o["tools"] = tools.Select(t => t.Name).OrderBy(n => n, StringComparer.Ordinal).ToList();

        var (launched, _) = await Call(client, log, "launch_browser", launchArgs);
        var m = Regex.Match(launched, @"^Launched (\S+) (\S+) \((\w+), (\d+) thread");
        if (m.Success) { o["browser"] = m.Groups[1].Value; o["version"] = m.Groups[2].Value; o["resource_class"] = m.Groups[3].Value; o["weight"] = int.Parse(m.Groups[4].Value); }
        var sid = sessionRe.Match(launched).Groups[1].Value;
        o["session_id"] = sid;
        o["navigate"] = FirstLine((await Navigate(client, log, "https://books.toscrape.com/")).Text);
        o["snapshot_chars"] = (await Call(client, log, "snapshot")).Text.Length;
        o["snapshot_list_chars"] = (await Call(client, log, "snapshot", new() { ["selector"] = "ol.row" })).Text.Length;
        o["html_chars"] = int.Parse((await Call(client, log, "evaluate", new() { ["expression"] = "document.documentElement.outerHTML.length" })).Text);
        var books = JsonDocument.Parse((await Call(client, log, "evaluate", new() { ["expression"] = Extract })).Text).RootElement;
        o["books"] = books.GetArrayLength();
        o["first_book"] = new Dictionary<string, string?> { ["title"] = books[0].GetProperty("title").GetString(), ["price"] = books[0].GetProperty("price").GetString() };
        o["closed"] = (await Call(client, log, "close_browser")).Text.StartsWith($"Session {sid} ended");
        var after = await Call(client, log, "navigate", new() { ["url"] = "https://books.toscrape.com/" });
        o["after_close"] = new Dictionary<string, object?> { ["error"] = after.Error, ["text"] = FirstLine(after.Text) };
        var account = JsonDocument.Parse((await Call(client, log, "account")).Text).RootElement;
        o["live_sessions_after"] = account.GetProperty("live_sessions").GetInt32();
    }
    o["calls"] = log;
    return o;
}

// ---- 2. A new MCP session for every tool call, as ChatGPT does.
async Task<Dictionary<string, object?>> PerCallSessions()
{
    var log = new List<Dictionary<string, object?>>();
    var sessions = new List<string?>();
    async Task<string> Once(string name, Dictionary<string, object?> args)
    {
        var sw = Stopwatch.StartNew();
        string text;
        await using (var client = await Connect(ownsSession: false)) // no DELETE: the client just goes away
        {
            text = (name == "navigate" ? await Navigate(client, log, (string)args["url"]!) : await Call(client, log, name, args)).Text;
            sessions.Add(client.SessionId);
        }
        log[^1]["with_initialize_ms"] = sw.ElapsedMilliseconds;
        return text;
    }
    var launched = await Once("launch_browser", launchArgs);
    var sid = sessionRe.Match(launched).Groups[1].Value;
    var nav = await Once("navigate", new() { ["url"] = "https://books.toscrape.com/catalogue/page-2.html" });
    var where = await Once("evaluate", new() { ["expression"] = "({ url: location.href, books: document.querySelectorAll(\"article.product_pod\").length })" });
    var closed = await Once("close_browser", new());
    return new()
    {
        ["mcp_sessions"] = sessions.Count,
        ["distinct_mcp_sessions"] = sessions.Distinct().Count(),
        ["browser_session_id"] = sid,
        ["navigate"] = FirstLine(nav),
        ["later_session_sees"] = JsonSerializer.Deserialize<Dictionary<string, object>>(where),
        ["closed_same_browser"] = closed.StartsWith($"Session {sid} ended"),
        ["calls"] = log,
    };
}

var one = await OneSession();
var perCall = await PerCallSessions();
Console.WriteLine(JsonSerializer.Serialize(new Dictionary<string, object?> { ["language"] = "csharp", ["one_session"] = one, ["per_call"] = perCall, ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd") },
    new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
