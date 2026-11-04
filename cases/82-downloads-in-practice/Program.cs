// dotnet add package ModelContextProtocol --version 2.3.0
// env: CDPFLEET_API_KEY (a paid plan: kept downloads aren't on Community), PROXY_URL
using System.Diagnostics;
using System.Net.WebSockets;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using ModelContextProtocol.Client;
using ModelContextProtocol.Protocol;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;
const string Api = "https://cdpfleet.com";
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(120) }; // the key is added per request: it goes only to cdpfleet
http.DefaultRequestHeaders.UserAgent.ParseAdd("cdpfleet-case-82/1.0"); // httpbingo.org answers 402 to requests without one
// Seeded test files: the same URL always returns the same bytes, so a copy can be checked
// against the original fetched directly from here.
string[] personFiles = ["https://httpbingo.org/bytes/204800?seed=82", "https://httpbingo.org/bytes/65536?seed=7"];
const string AgentFile = "https://httpbingo.org/bytes/102400?seed=79";

async Task<(int Status, JsonNode? Body)> ApiCall(HttpMethod method, string url, JsonObject? body = null)
{
    var req = new HttpRequestMessage(method, url);
    req.Headers.Add("x-api-key", key);
    if (body != null) req.Content = new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json");
    var res = await http.SendAsync(req);
    var text = await res.Content.ReadAsStringAsync();
    return ((int)res.StatusCode, text.Length > 0 ? JsonNode.Parse(text) : null);
}

static string Sha256(byte[] b) => Convert.ToHexString(SHA256.HashData(b)).ToLowerInvariant();

// Poll the account API until the session's kept files are listed; ms from `since`.
async Task<(JsonArray Files, long Ms)> Listed(string sessionId, int count, Stopwatch since)
{
    while (true)
    {
        var (_, body) = await ApiCall(HttpMethod.Get, $"{Api}/v1/me/downloads?session_id={sessionId}");
        var files = body!["files"]!.AsArray();
        if (files.Count >= count || since.ElapsedMilliseconds > 120000) return (files, since.ElapsedMilliseconds);
        await Task.Delay(250);
    }
}

// Fetch each kept file through its private link and compare it with what the list said and,
// for seeded files, with the original.
async Task<List<Dictionary<string, object?>>> FetchKept(JsonArray files, Dictionary<string, string> links)
{
    var output = new List<Dictionary<string, object?>>();
    foreach (var f in files.OrderByDescending(f => (long)f!["bytes"]!))
    {
        var name = (string)f!["name"]!;
        var source = (string)f["source_url"]!;
        string how = "from the downloads tool";
        if (!links.TryGetValue(name, out var url))
        {
            var (_, link) = await ApiCall(HttpMethod.Get, $"{Api}/v1/me/downloads/{(string)f["id"]!}/url");
            url = (string)link!["url"]!;
            how = $"GET /v1/me/downloads/{{id}}/url ({(int)link["expires_in_s"]!} s)";
        }
        var res = await http.GetAsync(url);
        var got = await res.Content.ReadAsByteArrayAsync();
        byte[]? original = Regex.IsMatch(source, "[?&]seed=") ? await http.GetByteArrayAsync(source) : null;
        output.Add(new()
        {
            ["name"] = name, ["bytes"] = (long)f["bytes"]!, ["sha256"] = (string)f["sha256"]!, ["source_url"] = source, ["link"] = how,
            ["fetched_status"] = (int)res.StatusCode, ["fetched_type"] = res.Content.Headers.ContentType?.ToString(),
            ["fetched_bytes"] = got.Length, ["fetched_sha256"] = Sha256(got),
            ["original_bytes"] = original?.Length, ["original_sha256"] = original == null ? null : Sha256(original),
        });
    }
    return output;
}

// ---- 1. A person in Launch interactive: no client, the server opens the start page and
//         a person drives it through a control live link (a scripted stand-in here).
async Task<Dictionary<string, object?>> Person()
{
    var (status, session) = await ApiCall(HttpMethod.Post, "https://starter.cdpfleet.com/chromium/session", new JsonObject
    {
        ["proxy"] = proxy, ["headless"] = true, ["live_view"] = true, ["record"] = false, ["downloads"] = true,
        ["start_url"] = "https://httpbingo.org/", ["inactivity_timeout"] = 60,
    });
    if (status != 200) throw new Exception($"launch {status} {session}");
    var sessionId = (string)session!["sessionId"]!;
    var host = new Uri((string)session["wsUrl"]!).Host;
    var (_, live) = await ApiCall(HttpMethod.Post, $"https://{host}/admin/session/live", new JsonObject { ["session_id"] = sessionId, ["mode"] = "control" });

    // The viewer: JPEG frames (binary) and JSON messages; "pages" says which URL is on screen.
    using var viewer = new ClientWebSocket();
    await viewer.ConnectAsync(new Uri((string)live!["live_url"]!), CancellationToken.None);
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
            if (r.MessageType == WebSocketMessageType.Binary) { Interlocked.Increment(ref frames); continue; }
            if (r.MessageType != WebSocketMessageType.Text) return;
            var m = JsonNode.Parse(Encoding.UTF8.GetString(ms.ToArray()))!;
            if ((string?)m["type"] != "pages") continue;
            var current = (string?)m["current"];
            var url = m["pages"]!.AsArray().Where(p => (string?)p!["targetId"] == current).Select(p => (string?)p!["url"]).FirstOrDefault();
            lock (seen) { if (url != null && (seen.Count == 0 || seen[^1] != url)) seen.Add(url); }
        }
    });
    bool Shown() { lock (seen) return seen.Contains("https://httpbingo.org/"); }
    for (var i = 0; i < 60 && !Shown(); i++) await Task.Delay(500);
    var startShown = Shown();

    // The person pastes two file links into the address bar, a few seconds apart.
    foreach (var url in personFiles)
    {
        var msg = new JsonObject { ["type"] = "navigate", ["url"] = url };
        await viewer.SendAsync(Encoding.UTF8.GetBytes(msg.ToJsonString()), WebSocketMessageType.Text, true, CancellationToken.None);
        await Task.Delay(3000);
    }
    string? pageAfter;
    lock (seen) pageAfter = seen.LastOrDefault();
    await viewer.CloseOutputAsync(WebSocketCloseStatus.NormalClosure, "", CancellationToken.None);

    // Closing the dashboard's viewer ends a Launch interactive session: DELETE /v1/me/threads/{id}.
    var sw = Stopwatch.StartNew();
    var (endStatus, _) = await ApiCall(HttpMethod.Delete, $"{Api}/v1/me/threads/{sessionId}");
    var endMs = sw.ElapsedMilliseconds;
    var (files, listedMs) = await Listed(sessionId, personFiles.Length, Stopwatch.StartNew());
    return new()
    {
        ["launch_downloads"] = (bool?)session["downloads"], ["resource_class"] = (string?)session["resource_class"], ["live_protocol"] = (string?)live["protocol"],
        ["start_page_shown"] = startShown, ["frames"] = frames, ["pasted"] = personFiles, ["page_after"] = pageAfter,
        ["end_status"] = endStatus, ["end_ms"] = endMs, ["listed_after_ms"] = listedMs, ["files"] = await FetchKept(files, new()),
    };
}

// ---- 2. An AI agent through the hosted MCP server, authenticated with the API key.
async Task<Dictionary<string, object?>> Agent()
{
    var calls = new List<Dictionary<string, object?>>();
    var text = "";
    var polls = 0;
    long toolMs;
    string? sessionId;
    await using (var client = await McpClient.CreateAsync(new HttpClientTransport(new HttpClientTransportOptions
    {
        Endpoint = new Uri($"{Api}/mcp"),
        TransportMode = HttpTransportMode.StreamableHttp,
        AdditionalHeaders = new Dictionary<string, string> { ["Authorization"] = $"Bearer {key}" },
        OwnsSession = true, // end the MCP session (DELETE) when done
    })))
    {
        async Task<string> Call(string name, Dictionary<string, object?>? args = null)
        {
            var sw = Stopwatch.StartNew();
            var r = await client.CallToolAsync(name, args ?? new Dictionary<string, object?>());
            var t = string.Join("\n", r.Content.OfType<TextContentBlock>().Select(c => c.Text));
            var first = t.Split('\n')[0];
            calls.Add(new() { ["tool"] = name, ["ms"] = sw.ElapsedMilliseconds, ["error"] = r.IsError == true, ["text"] = first.Length > 160 ? first[..160] : first });
            return t;
        }

        var launched = await Call("launch_browser", new() { ["browser"] = "chromium", ["headless"] = true, ["proxy"] = proxy, ["record"] = false, ["downloads"] = true });
        var sm = Regex.Match(launched, "session ([0-9a-f-]{36})");
        sessionId = sm.Success ? sm.Groups[1].Value : null;
        await Call("navigate", new() { ["url"] = "https://httpbingo.org/" });
        await Call("click", new() { ["role"] = "link", ["name"] = "/bytes/:n" }); // the index page's example link: 1,024 random bytes
        await Call("navigate", new() { ["url"] = AgentFile }); // a file link opened directly
        await Call("evaluate", new() { ["expression"] = "location.href" });
        await Task.Delay(2000); // an assistant's next call comes seconds later; let the downloads finish
        await Call("close_browser");

        // Ask the downloads tool until both files are there.
        var closed = Stopwatch.StartNew();
        while (closed.ElapsedMilliseconds < 120000)
        {
            polls++;
            text = await Call("downloads");
            if (Regex.Matches(text, " bytes, sha256 ").Count >= 2) break;
            await Task.Delay(500);
        }
        toolMs = closed.ElapsedMilliseconds;
    }

    // "<name> — <bytes> bytes, sha256 <hex>" and a private link on the next line.
    var links = new Dictionary<string, string>();
    var toolFiles = new List<Dictionary<string, object?>>();
    foreach (Match m in Regex.Matches(text, @"^(.+) — (\d+) bytes, sha256 ([0-9a-f]{64})\n {2}(https:\S+)", RegexOptions.Multiline))
    {
        links[m.Groups[1].Value] = m.Groups[4].Value;
        toolFiles.Add(new() { ["name"] = m.Groups[1].Value, ["bytes"] = long.Parse(m.Groups[2].Value), ["sha256"] = m.Groups[3].Value, ["link_host"] = new Uri(m.Groups[4].Value).Host });
    }
    var (files, _) = await Listed(sessionId!, 2, Stopwatch.StartNew());
    var last = calls[^1];
    last["polls"] = polls;
    return new()
    {
        ["session_id_found"] = sessionId != null,
        ["calls"] = calls.Where(c => (string)c["tool"]! != "downloads").Append(last).ToList(),
        ["tool_listed_after_ms"] = toolMs,
        ["first_downloads_call"] = calls.First(c => (string)c["tool"]! == "downloads")["text"],
        ["tool_files"] = toolFiles,
        ["files"] = await FetchKept(files, links),
    };
}

var person = await Person();
var agent = await Agent();
Console.WriteLine(JsonSerializer.Serialize(new Dictionary<string, object?> { ["language"] = "csharp", ["person"] = person, ["agent"] = agent, ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd") },
    new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
