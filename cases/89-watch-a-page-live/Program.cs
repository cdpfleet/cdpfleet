// .NET 9, no packages. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Api = "https://cdpfleet.com";
// A public page that changes on its own: it asks its server for the ISS position about once a second
// (XHR to /w/ajax/realtime) and rewrites a small table with it.
const string Page = "https://wheretheiss.at/";
const int StopAfterS = 40; // DELETE the job this long after starting it
string[] ended = ["done", "failed", "stopped", "interrupted"];
string[] types = ["xhr", "fetch", "ws", "dom"];
var uuidRe = new Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$");
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(240) };

async Task<(int Status, JsonObject Json, string Text)> Send(HttpMethod method, string url, JsonNode? body = null, bool withKey = true)
{
    var req = new HttpRequestMessage(method, url);
    if (withKey) req.Headers.Add("x-api-key", key);
    if (body != null) req.Content = new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json");
    var res = await http.SendAsync(req);
    var text = Encoding.UTF8.GetString(await res.Content.ReadAsByteArrayAsync());
    JsonObject json;
    try { json = JsonNode.Parse(text)!.AsObject(); } catch { json = new JsonObject(); }
    return ((int)res.StatusCode, json, text);
}
async Task<(int Status, JsonObject Json, string Text)> CallApi(HttpMethod method, string path, JsonNode? body = null) => await Send(method, Api + path, body);
string? Str(JsonNode? n) => n is JsonValue v && v.TryGetValue<string>(out var s) ? s : null;
double Num(JsonNode? n) => n is JsonValue v && v.TryGetValue<double>(out var d) ? d : 0;
double Secs(double ms) => Math.Round(ms / 100, MidpointRounding.AwayFromZero) / 10;
double Round(double x, int d) => Math.Round(x, d, MidpointRounding.AwayFromZero);
double Ms(JsonNode? iso) => DateTimeOffset.Parse(Str(iso)!).ToUnixTimeMilliseconds(); // an API timestamp
double NowMs() => DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
string Key(JsonNode e) => $"{Str(e["type"])} {Str(e["at"])} {Str(e["url"]) ?? Str(e["selector"])}";
JsonObject ByType(IEnumerable<JsonNode> events)
{
    var o = new JsonObject();
    foreach (var t in types) { var n = events.Count(e => Str(e["type"]) == t); if (n > 0) o[t] = n; }
    return o;
}

// x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>, checked on the raw bytes.
bool Verify(string secret, string? header, string rawBody)
{
    var parts = (header ?? "").Split(',').Select(kv => kv.Split('=', 2)).Where(p => p.Length == 2).ToDictionary(p => p[0], p => p[1]);
    if (!parts.TryGetValue("t", out var t) || !parts.TryGetValue("v1", out var v1)) return false;
    var expected = HMACSHA256.HashData(Encoding.UTF8.GetBytes(secret), Encoding.UTF8.GetBytes($"{t}.{rawBody}"));
    byte[] given;
    try { given = Convert.FromHexString(v1); } catch { return false; }
    return CryptographicOperations.FixedTimeEquals(expected, given);
}

// Great-circle distance in km.
double Km(double lat1, double lon1, double lat2, double lon2)
{
    double R(double d) => d * Math.PI / 180;
    var h = Math.Pow(Math.Sin(R(lat2 - lat1) / 2), 2) + Math.Cos(R(lat1)) * Math.Cos(R(lat2)) * Math.Pow(Math.Sin(R(lon2 - lon1) / 2), 2);
    return 2 * 6371 * Math.Asin(Math.Sqrt(h));
}

// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour (raw body and
// headers) and reads them back with GET. Use your own public https endpoint.
var hook = $"{Api}/e2e/hook/{Convert.ToBase64String(RandomNumberGenerator.GetBytes(18)).Replace('+', '-').Replace('/', '_').TrimEnd('=')}";
var secret = Str((await CallApi(HttpMethod.Get, "/v1/me/webhook-secret")).Json["secret"])!;

// 1. Start the watch: XHR/fetch calls matching the glob, and changes under the selector, with a webhook.
var watch = new JsonObject
{
    ["url"] = Page, ["browser"] = "chrome",
    ["duration"] = 300, // a safety net: the program stops the job itself
    ["reload_every"] = 15,
    ["filter"] = new JsonObject { ["url"] = new JsonArray("*/w/ajax/realtime*"), ["selector"] = ".span3 table.table-striped", ["types"] = new JsonArray("xhr", "fetch", "dom") },
};
var body = watch.DeepClone().AsObject();
body["deliver"] = new JsonObject { ["webhook"] = new JsonObject { ["url"] = hook, ["events"] = new JsonArray("event", "done") } };
var t0 = NowMs();
var start = await CallApi(HttpMethod.Post, "/v1/functions/watch", body);
if (start.Status != 202) throw new Exception($"watch {start.Status} {start.Text}");
var id = Str(start.Json["job_id"])!;

// 2. Follow it live on the SSE stream; stop it with DELETE after StopAfterS seconds.
JsonObject? stop = null;
double stopAt = 0;
using var cancelStop = new CancellationTokenSource();
var stopper = Task.Run(async () =>
{
    await Task.Delay(StopAfterS * 1000, cancelStop.Token);
    stopAt = NowMs();
    var r = await CallApi(HttpMethod.Delete, $"/v1/jobs/{id}");
    stop = new JsonObject { ["status"] = r.Status, ["reply"] = Str(r.Json["status"]) };
});

var live = new List<(JsonNode Ev, double Received)>(); // every event as it arrived
var seen = new Dictionary<string, int> { ["progress"] = 0, ["part"] = 0, ["event"] = 0, ["done"] = 0 };
JsonNode? doneData = null;
var sseReq = new HttpRequestMessage(HttpMethod.Get, $"{Api}/v1/jobs/{id}/events");
sseReq.Headers.Add("x-api-key", key);
sseReq.Headers.Add("accept", "text/event-stream");
using (var sse = await http.SendAsync(sseReq, HttpCompletionOption.ResponseHeadersRead))
using (var reader = new StreamReader(await sse.Content.ReadAsStreamAsync()))
{
    string? name = null;
    var data = new List<string>();
    while (await reader.ReadLineAsync() is string line)
    {
        if (line.Length > 0)
        {
            if (line.StartsWith("event:")) name = line[6..].Trim();
            else if (line.StartsWith("data:")) data.Add(line[5..].Trim());
            continue;
        }
        if (name != null) // a blank line ends a message; a keep-alive comment has no event name
        {
            seen[name] = seen.GetValueOrDefault(name) + 1;
            var payload = data.Count > 0 ? JsonNode.Parse(string.Join("\n", data)) : new JsonObject();
            if (name == "event") foreach (var ev in payload!["events"]!.AsArray()) live.Add((ev!.DeepClone(), NowMs()));
            if (name == "done") { doneData = payload; break; }
        }
        name = null;
        data.Clear();
    }
}
cancelStop.Cancel();
try { await stopper; } catch (TaskCanceledException) { /* the job ended first */ }
var tEnd = NowMs();

// 3. The job as it ended, and its events as stored.
var job = (await CallApi(HttpMethod.Get, $"/v1/jobs/{id}")).Json;
for (var i = 0; i < 20 && !ended.Contains(Str(job["status"])); i++) { await Task.Delay(2000); job = (await CallApi(HttpMethod.Get, $"/v1/jobs/{id}")).Json; }
var stored = new List<JsonNode>();
for (long cursor = 0; ;)
{
    var r = (await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/results?cursor={cursor}")).Json;
    stored.AddRange(r["results"]!.AsArray().Select(x => x!.DeepClone()));
    if (r["next_cursor"] is null || (long)Num(r["next_cursor"]) == cursor) break;
    cursor = (long)Num(r["next_cursor"]);
}

// 4. The webhook deliveries: job.event batches (at most one per 2 s) and job.done. Wait for job.done.
var items = new List<JsonNode>();
for (var end = NowMs() + 90000; NowMs() < end; await Task.Delay(2000))
{
    items = JsonNode.Parse((await Send(HttpMethod.Get, hook, withKey: false)).Text)!["items"]!.AsArray().Select(x => x!.DeepClone()).ToList();
    if (items.Any(d => Str(d["event"]) == "job.done")) break;
}
var hooks = items.Select(d => (D: d, Json: JsonNode.Parse(Str(d["body"])!)!)).ToList();
var batches = hooks.Where(h => Str(h.D["event"]) == "job.event").ToList();
var doneHook = hooks.FirstOrDefault(h => Str(h.D["event"]) == "job.done");
var hookEvents = batches.SelectMany(h => h.Json["data"]!["events"]!.AsArray().Select(e => e!)).ToList();
var sent = batches.Select(h => Ms(h.Json["created_at"])).OrderBy(x => x).ToList();
var gaps = sent.Skip(1).Select((t, i) => t - sent[i]).ToList();
// How long after its newest event each batch was signed (the signature's t, whole seconds).
var delays = batches.Select(h => long.Parse(Regex.Match(Str(h.D["signature"]) ?? "", @"t=(\d+)").Groups[1].Value)
    - h.Json["data"]!["events"]!.AsArray().Max(e => Ms(e!["at"])) / 1000).OrderBy(x => x).ToList();
var storedKeys = stored.Select(Key).ToHashSet();
var hookKeys = hookEvents.Select(Key).ToHashSet();

// 5. What the events say: the ISS position from each XHR response, the table text from each DOM change.
// An XHR still in flight when the page reloads is reported without a response body.
var positions = stored.Where(e => Str(e["type"]) == "xhr" && Str(e["response_body"]) != null).Select(e =>
{
    var d = JsonNode.Parse(Str(e["response_body"])!)!["data"]!;
    return (At: Ms(e["at"]), Lat: Num(d["lat"]), Lon: Num(d["lon"]), Time: Str(d["time"]));
}).ToList();
var lags = live.Select(x => (x.Received - Ms(x.Ev["at"])) / 1000).OrderBy(x => x).ToList();
var dom = stored.Where(e => Str(e["type"]) == "dom").ToList();
JsonNode? FirstOf(string t) => live.Where(x => Str(x.Ev["type"]) == t).Select(x => (JsonNode?)Secs(x.Received - t0)).FirstOrDefault();

// 6. The bill: a watch runs in your own browser session, one thread for a headless Chrome.
var since = Uri.EscapeDataString(DateTimeOffset.FromUnixTimeMilliseconds((long)t0 - 60000).UtcDateTime.ToString("yyyy-MM-ddTHH:mm:ss.fffZ"));
var sessions = (await CallApi(HttpMethod.Get, $"/v1/me/sessions?from={since}&limit=200")).Json["sessions"]?.AsArray() ?? new JsonArray();
var sessionId = Str(job["session_id"]);
var session = sessions.FirstOrDefault(s => sessionId != null && (Str(s!["session_id"]) == sessionId || Str(s!["id"]) == sessionId));

var watchOut = watch.DeepClone().AsObject();
watchOut["stop_after_s"] = StopAfterS;
watchOut["start_status"] = start.Status;
var first = positions.Count > 0 ? positions[0] : default;
var last = positions.Count > 0 ? positions[^1] : default;
var dist = positions.Count > 0 ? Km(first.Lat, first.Lon, last.Lat, last.Lon) : 0;
var output = new JsonObject
{
    ["language"] = "csharp",
    ["page"] = Page,
    ["watch"] = watchOut,
    ["sse"] = new JsonObject
    {
        ["events"] = live.Count, ["by_type"] = ByType(live.Select(x => x.Ev)),
        ["messages"] = new JsonObject(seen.Select(kv => KeyValuePair.Create(kv.Key, (JsonNode?)kv.Value))),
        ["first_event_seconds"] = live.Count > 0 ? Secs(live[0].Received - t0) : null,
        ["first_xhr_seconds"] = FirstOf("xhr"), ["first_dom_seconds"] = FirstOf("dom"),
        ["lag_seconds"] = lags.Count > 0 ? new JsonObject { ["min"] = Round(lags[0], 2), ["median"] = Round(lags[lags.Count / 2], 2), ["max"] = Round(lags[^1], 2) } : null,
        ["done_status"] = Str(doneData?["status"]), ["stream_seconds"] = Secs(tEnd - t0),
    },
    ["stop"] = stop == null ? null : new JsonObject
    {
        ["after_seconds"] = Secs(stopAt - t0), ["status"] = stop["status"]!.DeepClone(), ["reply"] = stop["reply"]?.DeepClone(),
        ["seconds_to_ended"] = Secs(Ms(job["ended_at"]) - stopAt),
    },
    ["job"] = new JsonObject
    {
        ["status"] = job["status"]?.DeepClone(), ["own_session"] = job["own_session"]?.DeepClone(), ["concurrency"] = job["concurrency"]?.DeepClone(),
        ["runs"] = job["runs"]?.DeepClone(), ["progress"] = job["progress"]?.DeepClone(), ["billed_seconds"] = job["billed_seconds"]?.DeepClone(),
        ["run_seconds"] = Secs(Ms(job["ended_at"]) - Ms(job["started_at"])),
        ["results"] = job["results"] is JsonObject res ? new JsonObject { ["parts"] = res["parts"]?.DeepClone(), ["bytes"] = res["bytes"]?.DeepClone() } : null,
        ["error"] = job["error"]?.DeepClone(),
    },
    ["session"] = session == null ? null : new JsonObject
    {
        ["engine"] = session["engine"]?.DeepClone(), ["weight"] = session["weight"]?.DeepClone(), ["seconds"] = Secs(Ms(session["ended_at"]) - Ms(session["started_at"])),
    },
    ["stored"] = new JsonObject
    {
        ["events"] = stored.Count, ["by_type"] = ByType(stored),
        ["same_as_live"] = stored.Count == live.Count && live.All(x => storedKeys.Contains(Key(x.Ev))),
        ["xhr_urls_match_glob"] = stored.Where(e => Str(e["type"]) == "xhr").All(e => Str(e["url"])!.Contains("/w/ajax/realtime") && Num(e["status"]) == 200 && Str(e["method"]) == "GET"),
        ["xhr_without_body"] = stored.Count(e => Str(e["type"]) == "xhr" && Str(e["response_body"]) == null),
        ["positions"] = positions.Count, ["distinct_positions"] = positions.Select(p => $"{p.Lat},{p.Lon}").Distinct().Count(),
        ["first"] = positions.Count > 0 ? new JsonObject { ["lat"] = Round(first.Lat, 3), ["lon"] = Round(first.Lon, 3), ["time"] = first.Time } : null,
        ["last"] = positions.Count > 0 ? new JsonObject { ["lat"] = Round(last.Lat, 3), ["lon"] = Round(last.Lon, 3), ["time"] = last.Time } : null,
        ["ground_km"] = positions.Count > 0 ? Round(dist, 1) : null,
        ["ground_km_per_s"] = positions.Count > 0 && last.At > first.At ? Round(dist / ((last.At - first.At) / 1000), 2) : null,
        ["dom_events"] = dom.Count,
        ["dom_text_sample"] = dom.Count > 0 ? Regex.Replace(Str(dom[0]["text"])!, @"\s+", " ").Trim() : null,
        ["dom_html_bytes"] = dom.Count > 0 ? Encoding.UTF8.GetByteCount(Str(dom[0]["html"])!) : null,
    },
    ["webhook"] = new JsonObject
    {
        ["deliveries"] = hooks.Count, ["inbox_full"] = items.Count >= 20,
        ["by_event"] = new JsonObject(hooks.GroupBy(h => Str(h.D["event"])!).Select(g => KeyValuePair.Create(g.Key, (JsonNode?)g.Count()))),
        ["signatures_valid"] = hooks.Count(h => Verify(secret, Str(h.D["signature"]), Str(h.D["body"])!)),
        ["tampered_body_rejected"] = hooks.Count > 0 && !Verify(secret, Str(hooks[0].D["signature"]), Str(hooks[0].D["body"]) + " "),
        ["delivery_ids_unique_uuids"] = hooks.Select(h => Str(h.D["delivery"])).Distinct().Count() == hooks.Count && hooks.All(h => uuidRe.IsMatch(Str(h.D["delivery"]) ?? "")),
        ["job_id_matches"] = hooks.All(h => Str(h.Json["job_id"]) == id),
        ["events"] = hookEvents.Count, ["events_by_type"] = ByType(hookEvents), ["events_all_stored"] = hookEvents.All(e => storedKeys.Contains(Key(e))),
        ["stored_not_in_webhooks"] = stored.Count(e => !hookKeys.Contains(Key(e))),
        ["not_sent_are_the_last"] = stored.Where(e => !hookKeys.Contains(Key(e))).All(e => hookEvents.All(h => Ms(h["at"]) <= Ms(e["at"]))),
        ["signed_after_newest_event_seconds"] = delays.Count > 0 ? new JsonObject { ["min"] = Round(delays[0], 1), ["max"] = Round(delays[^1], 1) } : null,
        ["min_gap_seconds"] = gaps.Count > 0 ? Secs(gaps.Min()) : null,
        ["max_events_per_batch"] = batches.Select(h => h.Json["data"]!["events"]!.AsArray().Count).DefaultIfEmpty(0).Max(),
        ["done"] = doneHook.Json is JsonNode dj ? new JsonObject
        {
            ["status"] = dj["data"]?["status"]?.DeepClone(), ["progress"] = dj["data"]?["progress"]?.DeepClone(), ["error"] = dj["data"]?["error"]?.DeepClone(),
        } : null,
    },
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
};
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
