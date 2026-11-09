// .NET 9, no packages. env: CDPFLEET_API_KEY (a paid plan: monitors aren't on Community)
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Api = "https://cdpfleet.com";
const string Page = "https://quotes.toscrape.com/random"; // a practice site: a different quote on every load
const int MaxRuns = 5; // checks to trigger at most while waiting for a change (the same quote can come up twice)
var uuidRe = new Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$");
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(120) };
var since = DateTime.UtcNow.AddSeconds(-60).ToString("o");

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
async Task<string> Get(string url) => (await Send(HttpMethod.Get, url, withKey: false)).Text;
double Num(JsonNode? n) => n is JsonValue v && v.TryGetValue<double>(out var d) ? d : 0;
string? Str(JsonNode? n) => n is JsonValue v && v.TryGetValue<string>(out var s) ? s : null;
double Secs(double ms) => Math.Round(ms / 100, MidpointRounding.AwayFromZero) / 10;
double Ms(JsonNode? iso) => Str(iso) is string s ? DateTimeOffset.Parse(s).ToUnixTimeMilliseconds() : 0; // an API timestamp
double NowMs() => DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
JsonNode? Copy(JsonNode? n) => n?.DeepClone();

// x-cdpfleet-signature: t=<unix>,v1=<hex HMAC-SHA256(secret, "<t>.<raw body>")>. Check it against the raw
// bytes received, before parsing them, and refuse old timestamps (replays).
bool Verify(string secret, string? header, string rawBody)
{
    var parts = (header ?? "").Split(',').Select(kv => kv.Split('=', 2)).Where(p => p.Length == 2).ToDictionary(p => p[0], p => p[1]);
    if (!parts.TryGetValue("t", out var t) || !parts.TryGetValue("v1", out var v1)) return false;
    var expected = HMACSHA256.HashData(Encoding.UTF8.GetBytes(secret), Encoding.UTF8.GetBytes($"{t}.{rawBody}"));
    byte[] given;
    try { given = Convert.FromHexString(v1); } catch { return false; }
    return CryptographicOperations.FixedTimeEquals(expected, given) && Math.Abs(DateTimeOffset.UtcNow.ToUnixTimeSeconds() - long.Parse(t)) < 300;
}

// The webhook receiver here is cdpfleet.com's own test inbox: it keeps deliveries an hour, raw body and
// headers, and reads them back with GET. Use your own public https endpoint.
var hook = $"{Api}/e2e/hook/{Convert.ToBase64String(RandomNumberGenerator.GetBytes(18)).Replace('+', '-').Replace('/', '_').TrimEnd('=')}";
async Task<JsonArray> Inbox() => JsonNode.Parse(await Get(hook))!["items"]!.AsArray();
var secret = Str((await CallApi(HttpMethod.Get, "/v1/me/webhook-secret")).Json["secret"])!;

// 1. Create the monitor: only the quote element, as text; the tags dropped (remove) and the "(about)" link
// label taken out of the text (ignore) before comparing.
// every: "1h" is its own schedule; the program doesn't wait for it and triggers checks with POST …/run.
var spec = new JsonObject
{
    ["url"] = Page, ["format"] = "text", ["selector"] = ".quote", ["remove"] = new JsonArray(".tags"), ["ignore"] = new JsonArray(@"\(about\)"), ["every"] = "1h",
    ["deliver"] = new JsonObject { ["webhook"] = new JsonObject { ["url"] = hook } },
};
var created = await CallApi(HttpMethod.Post, "/v1/monitors", spec);
if (created.Status != 201) throw new Exception($"create {created.Status} {created.Text}");
var c = created.Json;
var id = Str(c["monitor_id"])!;
var output = new JsonObject { ["language"] = "csharp", ["page"] = Page };
try
{
    var listed = (await CallApi(HttpMethod.Get, "/v1/monitors")).Json["monitors"]!.AsArray().Any(m => Str(m!["monitor_id"]) == id);
    output["monitor"] = new JsonObject
    {
        ["create_status"] = created.Status, ["every"] = Copy(c["every"]), ["format"] = Copy(c["format"]), ["selector"] = Copy(c["selector"]),
        ["remove"] = Copy(c["remove"]), ["ignore"] = Copy(c["ignore"]), ["webhook"] = Str(c["deliver"]?["webhook"]?["url"]) == hook, ["active"] = Copy(c["active"]), ["listed"] = listed,
        ["first_check_due_seconds"] = Secs(Ms(c["next_run_at"]) - Ms(c["created_at"])),
    };

    // 2. Check now, and again until the page has changed. Poll the monitor and the inbox every 0.5 s.
    var checks = new List<JsonObject>();
    var windows = new List<(double Run, double LastRun)>();
    int changeN = 0;
    double changeRun = 0, tHook = 0;
    JsonObject? item = null;
    for (var n = 1; n <= MaxRuns; n++)
    {
        var tRun = NowMs();
        var run = await CallApi(HttpMethod.Post, $"/v1/monitors/{id}/run");
        JsonObject m = new();
        double tStatus = 0;
        for (var end = DateTime.UtcNow.AddSeconds(120); DateTime.UtcNow < end; await Task.Delay(500))
        {
            if (tStatus == 0)
            {
                m = (await CallApi(HttpMethod.Get, $"/v1/monitors/{id}")).Json;
                if (Num(m["checks"]) >= n) tStatus = NowMs();
            }
            if (tStatus != 0 && Str(m["last_status"]) != "changed") break;
            if (item == null)
            {
                item = (await Inbox()).FirstOrDefault(d => Str(d!["event"]) == "monitor.change")?.AsObject();
                if (item != null) tHook = NowMs();
            }
            if (tStatus != 0 && item != null) break;
        }
        if (tStatus == 0) throw new Exception($"check {n}: no result after 120 s");
        checks.Add(new JsonObject
        {
            ["n"] = n, ["run_status"] = run.Status, ["status"] = Copy(m["last_status"]), ["error"] = Copy(m["last_error"]),
            ["seconds_to_status"] = Secs(tStatus - tRun), ["check_seconds"] = Secs(Ms(m["last_run_at"]) - tRun),
        });
        windows.Add((tRun, Ms(m["last_run_at"])));
        if (Str(m["last_status"]) == "changed") { changeN = n; changeRun = tRun; break; }
    }

    // 3. The webhook: verify it, then compare it with the change as the API keeps it.
    var deliveries = await Inbox();
    string? changeId = null;
    if (item != null)
    {
        var raw = Str(item["body"])!;
        var body = JsonNode.Parse(raw)!.AsObject();
        var data = body["data"]!.AsObject();
        changeId = Str(data["change_id"]);
        var t = long.Parse(Regex.Match(Str(item["signature"]) ?? "", @"t=(\d+)").Groups[1].Value);
        var full = (await CallApi(HttpMethod.Get, $"/v1/monitors/{id}/changes/{changeId}")).Json;
        var before = Str(full["before_url"]) is string bu ? await Get(bu) : null;
        var after = await Get(Str(full["after_url"])!);
        var snapshot = await Get(Str(data["snapshot_url"])!);
        var diff = Str(data["diff"])!;
        var lines = diff.Split('\n');
        output["webhook"] = new JsonObject
        {
            ["deliveries"] = deliveries.Count, ["event_header"] = Copy(item["event"]), ["type"] = Copy(body["type"]),
            ["delivery_id_is_uuid"] = uuidRe.IsMatch(Str(item["delivery"]) ?? ""),
            ["signature_valid"] = Verify(secret, Str(item["signature"]), raw),
            ["tampered_body_rejected"] = !Verify(secret, Str(item["signature"]), new Regex("\"added\":").Replace(raw, "\"added\": ", 1)),
            ["monitor_id_matches"] = Str(data["monitor_id"]) == id, ["url"] = Copy(data["url"]),
            ["keys"] = new JsonArray(data.Select(kv => (JsonNode)JsonValue.Create(kv.Key)!).OrderBy(k => (string)k!, StringComparer.Ordinal).ToArray()),
            ["seconds_run_to_seen"] = Secs(tHook - changeRun),
            ["seconds_run_to_signed"] = t - (long)changeRun / 1000,
            ["signed_within_1s_of_change"] = Math.Abs(t * 1000 - Ms(data["at"])) < 1000,
        };
        output["change"] = new JsonObject
        {
            ["after_runs"] = changeN, ["change_id"] = changeId, ["added"] = Copy(data["added"]), ["removed"] = Copy(data["removed"]),
            ["diff_bytes"] = Encoding.UTF8.GetByteCount(diff), ["diff_lines"] = lines.Length,
            ["removed_lines"] = new JsonArray(lines.Where(l => l.StartsWith('-') && !l.StartsWith("---")).Select(l => (JsonNode)JsonValue.Create(l[1..])!).ToArray()),
            ["added_lines"] = new JsonArray(lines.Where(l => l.StartsWith('+') && !l.StartsWith("+++")).Select(l => (JsonNode)JsonValue.Create(l[1..])!).ToArray()),
            ["diff_has_ignored_text"] = Regex.IsMatch(diff, @"Tags:|\(about\)"),
            ["api_diff_equal"] = Str(full["diff"]) == diff,
            ["api_counts_equal"] = Num(full["added"]) == Num(data["added"]) && Num(full["removed"]) == Num(data["removed"]),
            ["snapshot_equals_after"] = snapshot == after,
            ["before_lines"] = before?.Split('\n').Length, ["after_lines"] = after.Split('\n').Length,
            ["after_text"] = after,
        };
    }

    // 4. The history: every change, newest first, with a preview of its diff.
    var ids = (await CallApi(HttpMethod.Get, $"/v1/monitors/{id}/changes")).Json["changes"]!.AsArray().Select(x => Str(x!["change_id"])!).ToList();
    var mon = (await CallApi(HttpMethod.Get, $"/v1/monitors/{id}")).Json;
    output["history"] = new JsonObject
    {
        ["changes"] = ids.Count, ["change_ids"] = new JsonArray(ids.Select(x => (JsonNode)JsonValue.Create(x)!).ToArray()),
        ["newest_is_webhook_change"] = ids.Count > 0 && ids[0] == changeId,
        ["monitor_checks"] = Copy(mon["checks"]), ["monitor_changes"] = Copy(mon["changes"]), ["last_status"] = Copy(mon["last_status"]),
        ["next_run_minutes_after_last_check"] = Math.Round((Ms(mon["next_run_at"]) - Ms(mon["last_run_at"])) / 60000),
    };

    // 5. Pause it (PATCH active: false): no more scheduled checks, history kept.
    var paused = await CallApi(HttpMethod.Patch, $"/v1/monitors/{id}", new JsonObject { ["active"] = false });
    output["pause"] = new JsonObject { ["status"] = paused.Status, ["active"] = Copy(paused.Json["active"]), ["next_run_at"] = Copy(paused.Json["next_run_at"]) };

    // 6. What the checks cost. Each check is a scrape the service runs as you, billed like any call: it has no
    // x-call-id you can see, but it is in the account's session history as engine "function:scrape".
    await Task.Delay(3000);
    var sessions = (await CallApi(HttpMethod.Get, $"/v1/me/sessions?limit=200&from={Uri.EscapeDataString(since)}")).Json["sessions"]!.AsArray().Select(x => x!.AsObject()).ToList();
    var billing = new JsonArray();
    for (var i = 0; i < checks.Count; i++)
    {
        var w = windows[i];
        var inside = sessions.Where(x => Str(x["engine"]) == "function:scrape" && Ms(x["started_at"]) >= w.Run - 2000 && Ms(x["started_at"]) <= w.LastRun)
            .OrderBy(x => Ms(x["started_at"])).ToList();
        var s = inside.FirstOrDefault(); // the first scrape to start after the run request
        double? dur = s != null && s["ended_at"] != null ? Ms(s["ended_at"]) - Ms(s["started_at"]) : null;
        billing.Add(new JsonObject
        {
            ["check"] = i + 1, ["sessions_in_window"] = inside.Count, ["engine"] = Copy(s?["engine"]), ["weight"] = Copy(s?["weight"]),
            ["seconds"] = dur is double d1 ? (JsonNode)Secs(d1) : null, ["thread_seconds"] = dur is double d2 ? (JsonNode)Secs(d2 * Num(s!["weight"])) : null,
        });
    }
    output["billing"] = billing;
    output["checks"] = new JsonArray(checks.Select(x => (JsonNode)x).ToArray());
}
finally
{
    // 7. Delete it, whatever happened above.
    var del = await CallApi(HttpMethod.Delete, $"/v1/monitors/{id}");
    var gone = await CallApi(HttpMethod.Get, $"/v1/monitors/{id}");
    output["deleted"] = new JsonObject { ["status"] = del.Status, ["deleted"] = del.Json["deleted"] is JsonValue dv && dv.GetValue<bool>(), ["get_after"] = gone.Status };
}
output["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd");
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
