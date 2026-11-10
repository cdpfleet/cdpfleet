// .NET 9, no packages. env: CDPFLEET_API_KEY (a paid plan: jobs aren't on Community)
using System.IO.Compression;
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Api = "https://cdpfleet.com";
const string Site = "https://books.toscrape.com/"; // a sandbox made for scraping practice
const int StopAt = 30; // stop the job once this many pages are done, then resume it
var crawl = new JsonObject { ["max_depth"] = 2, ["max_pages"] = 100, ["concurrency"] = 4, ["delay_ms"] = 250, ["formats"] = new JsonArray("markdown") };
var ended = new HashSet<string> { "done", "failed", "stopped", "interrupted" };
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(120) };

async Task<HttpResponseMessage> Send(HttpMethod method, string url, JsonNode? body = null, bool withKey = true)
{
    var req = new HttpRequestMessage(method, url);
    if (withKey) req.Headers.Add("x-api-key", key);
    if (body != null) req.Content = new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json");
    return await http.SendAsync(req);
}
async Task<JsonObject> Json(HttpResponseMessage r)
{
    try { return JsonNode.Parse(await r.Content.ReadAsStringAsync())!.AsObject(); } catch { return new JsonObject(); }
}
async Task<JsonObject> CallApi(HttpMethod method, string path, JsonNode? body = null) => await Json(await Send(method, Api + path, body));
double Num(JsonNode? n) => n is JsonValue v && v.TryGetValue<double>(out var d) ? d : 0;
string? Str(JsonNode? n) => n is JsonValue v && v.TryGetValue<string>(out var s) ? s : null;
double Round1(double v) => Math.Round(v, 1, MidpointRounding.AwayFromZero);
JsonNode? Copy(JsonNode? n) => n?.DeepClone();

// Poll the job every 3 s until `until` holds.
async Task<JsonObject> WaitFor(string id, Func<JsonObject, bool> until, int seconds)
{
    JsonObject? job = null;
    for (var end = DateTime.UtcNow.AddSeconds(seconds); DateTime.UtcNow < end; await Task.Delay(3000))
    {
        job = await CallApi(HttpMethod.Get, $"/v1/jobs/{id}");
        if (until(job)) return job;
    }
    throw new Exception($"job {id}: still {Str(job?["status"])} after {seconds} s");
}
async Task<List<JsonObject>> Stored(string id) // results stored so far, part by part
{
    var rows = new List<JsonObject>();
    JsonNode? cursor = 0;
    while (cursor != null)
    {
        var r = await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/results?cursor={cursor.ToJsonString()}");
        rows.AddRange(r["results"]!.AsArray().Select(x => x!.AsObject()));
        if (r["next_cursor"]?.ToJsonString() == cursor.ToJsonString()) break; // nothing new yet
        cursor = Copy(r["next_cursor"]);
    }
    return rows;
}
async Task<JsonArray> HookItems(string hook) => (await Json(await Send(HttpMethod.Get, hook, withKey: false)))["items"]!.AsArray();

// 1. Map: the site's URLs in one instant call (sitemaps + the start page's links).
var t = DateTime.UtcNow;
var mapRes = await Send(HttpMethod.Post, $"{Api}/v1/functions/map", new JsonObject { ["url"] = Site, ["max_urls"] = 5000 });
var m = await Json(mapRes);
var mapUrls = m["urls"]?.AsArray().Select(u => (string)u!).ToList() ?? new List<string>();
var mapOut = new JsonObject
{
    ["status"] = (int)mapRes.StatusCode, ["seconds"] = Round1((DateTime.UtcNow - t).TotalSeconds), ["urls"] = mapUrls.Count,
    ["sources"] = Copy(m["sources"]), ["partial"] = mapRes.Headers.TryGetValues("x-cdpfleet-partial", out var p) ? p.First() : null,
    ["categories"] = mapUrls.Count(u => u.Contains("/catalogue/category/")),
};

// 2. Start the crawl as a job: a zip archive at the end, and webhooks when it's done and when the zip is ready.
// The receiver here is cdpfleet.com's own test inbox (keeps deliveries an hour, read back with GET);
// use your own public https endpoint.
var hook = $"{Api}/e2e/hook/{Convert.ToBase64String(RandomNumberGenerator.GetBytes(18)).Replace('+', '-').Replace('/', '_').TrimEnd('=')}";
var body = crawl.DeepClone().AsObject();
body["url"] = Site;
body["extract"] = new JsonObject { ["title"] = "h1", ["price"] = ".product_main .price_color" };
body["deliver"] = new JsonObject { ["webhook"] = new JsonObject { ["url"] = hook, ["events"] = new JsonArray("done", "archive") }, ["format"] = "archive" };
var tStart = DateTime.UtcNow;
var startRes = await Send(HttpMethod.Post, $"{Api}/v1/functions/crawl", body);
if ((int)startRes.StatusCode != 202) throw new Exception($"crawl {(int)startRes.StatusCode} {await startRes.Content.ReadAsStringAsync()}");
var id = (string)(await Json(startRes))["job_id"]!;

// 3. Watch it, and stop it part-way.
var before = await WaitFor(id, j => Num(j["progress"]?["done"]) >= StopAt || ended.Contains(Str(j["status"]) ?? ""), 600);
var stopReply = await CallApi(HttpMethod.Delete, $"/v1/jobs/{id}");
var tStop = DateTime.UtcNow;
var stopped = await WaitFor(id, j => ended.Contains(Str(j["status"]) ?? ""), 300);
var tStopped = DateTime.UtcNow;
var atStop = await Stored(id);
var hooksAtStop = new JsonArray((await HookItems(hook)).Select(d => (JsonNode?)$"{Str(d!["event"])} {Str(JsonNode.Parse(Str(d["body"])!)!["data"]?["status"])}").ToArray());
var archiveAtStop = await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/archive");

// 4. Resume: a new run of the same job, from its checkpoint.
var tResume = DateTime.UtcNow;
var resumeRes = await Send(HttpMethod.Post, $"{Api}/v1/jobs/{id}/resume");
var resume = await Json(resumeRes);
// 5. Wait for the second run to end.
var done = await WaitFor(id, j => Num(j["runs"]) >= 2 && ended.Contains(Str(j["status"]) ?? ""), 900);
var tDone = DateTime.UtcNow;

// 6a. Every result as one JSON-lines file.
var jsonl = await (await Send(HttpMethod.Get, $"{Api}/v1/jobs/{id}/results.jsonl")).Content.ReadAsByteArrayAsync();
var rows = Encoding.UTF8.GetString(jsonl).Trim().Split('\n').Select(l => JsonNode.Parse(l)!.AsObject()).ToList();
var urls = rows.Select(r => Str(r["url"])!).ToHashSet();
var firstRun = atStop.Select(r => Str(r["url"])!).ToHashSet();
var depths = new JsonObject();
foreach (var g in rows.GroupBy(r => r["depth"]?.ToJsonString() ?? "null").OrderBy(g => g.Key)) depths[g.Key] = g.Count();

// 6b. The same results as a zip (built when the job ended).
await WaitFor(id, j => Str(j["archive"]?["status"]) is "ready" or "failed", 300);
var archive = await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/archive");
var zipBytes = await (await Send(HttpMethod.Get, Str(archive["url"])!, withKey: false)).Content.ReadAsByteArrayAsync(); // a storage link, no key
List<string> names;
var indexLines = 0;
using (var zip = new ZipArchive(new MemoryStream(zipBytes), ZipArchiveMode.Read))
{
    names = zip.Entries.Select(e => e.FullName).ToList();
    var entry = zip.GetEntry("index.jsonl");
    if (entry != null)
    {
        using var reader = new StreamReader(entry.Open(), Encoding.UTF8);
        var text = (await reader.ReadToEndAsync()).Trim();
        indexLines = text.Length == 0 ? 0 : text.Split('\n').Length;
    }
}

// 7. The webhooks: every delivery is signed with the account's secret, as HMAC-SHA256(secret, "<t>.<raw body>").
var secret = Str((await CallApi(HttpMethod.Get, "/v1/me/webhook-secret"))["secret"])!;
var deliveries = new JsonArray();
JsonNode? finalData = null, archiveData = null;
long finalSignedAt = 0, archiveSignedAt = 0;
for (var end = DateTime.UtcNow.AddSeconds(90); DateTime.UtcNow < end && (finalData == null || archiveData == null);)
{
    deliveries = new JsonArray();
    finalData = null;
    archiveData = null;
    foreach (var d in await HookItems(hook))
    {
        var parts = (Str(d!["signature"]) ?? "").Split(',').Select(kv => kv.Split('=', 2)).Where(a => a.Length == 2).ToDictionary(a => a[0], a => a[1]);
        var raw = Str(d["body"])!;
        var b = JsonNode.Parse(raw)!;
        var want = Convert.ToHexStringLower(HMACSHA256.HashData(Encoding.UTF8.GetBytes(secret), Encoding.UTF8.GetBytes($"{parts.GetValueOrDefault("t")}.{raw}")));
        deliveries.Add(new JsonObject
        {
            ["event"] = Str(d["event"]), ["status"] = Str(b["data"]?["status"]), ["job_id_matches"] = Str(b["job_id"]) == id,
            ["delivery_id_is_uuid"] = Regex.IsMatch(Str(d["delivery"]) ?? "", "^[0-9a-f-]{36}$"),
            ["signature_valid"] = parts.TryGetValue("v1", out var v1) && CryptographicOperations.FixedTimeEquals(Encoding.ASCII.GetBytes(v1), Encoding.ASCII.GetBytes(want)),
        });
        if (finalData == null && Str(d["event"]) == "job.done" && Str(b["data"]?["status"]) == "done")
        {
            finalData = Copy(b["data"]);
            finalSignedAt = long.Parse(parts["t"]);
        }
        if (archiveData == null && Str(d["event"]) == "job.archive")
        {
            archiveData = Copy(b["data"]);
            archiveSignedAt = long.Parse(parts["t"]);
        }
    }
    if (finalData == null || archiveData == null) await Task.Delay(3000);
}
// job.archive carries the zip's size and a download link, like GET /v1/jobs/{id}/archive.
var hookZipUrl = Str(archiveData?["url"]);
int? hookZipBytes = hookZipUrl == null ? null : (await (await Send(HttpMethod.Get, hookZipUrl, withKey: false)).Content.ReadAsByteArrayAsync()).Length;

// 8. What it cost: a run holds `concurrency` threads, billed_seconds is the total in thread-seconds.
var billed = Num(done["billed_seconds"]);
var runSeconds = billed / Num(done["concurrency"]);
var crawlOut = crawl.DeepClone().AsObject();
crawlOut["extract"] = new JsonArray("title", "price");
crawlOut["start_status"] = (int)startRes.StatusCode;

Console.WriteLine(new JsonObject
{
    ["language"] = "csharp",
    ["site"] = Site,
    ["map"] = mapOut,
    ["crawl"] = crawlOut,
    ["stop"] = new JsonObject
    {
        ["requested_at_done"] = Num(before["progress"]?["done"]), ["reply"] = Str(stopReply["status"]),
        ["seconds_to_stopped"] = Round1((tStopped - tStop).TotalSeconds), ["status"] = Str(stopped["status"]), ["progress"] = Copy(stopped["progress"]),
        ["stored_results"] = atStop.Count, ["webhooks_by_then"] = hooksAtStop, ["archive"] = Str(archiveAtStop["status"]),
    },
    ["resume"] = new JsonObject { ["status"] = (int)resumeRes.StatusCode, ["reply"] = Str(resume["status"]) },
    ["job"] = new JsonObject
    {
        ["status"] = Copy(done["status"]), ["runs"] = Copy(done["runs"]), ["progress"] = Copy(done["progress"]), ["concurrency"] = Copy(done["concurrency"]),
        ["billed_seconds"] = Copy(done["billed_seconds"]), ["results"] = new JsonObject { ["parts"] = Copy(done["results"]?["parts"]), ["bytes"] = Copy(done["results"]?["bytes"]) },
        ["started_at"] = Copy(done["started_at"]), ["ended_at"] = Copy(done["ended_at"]), ["expires_at"] = Copy(done["expires_at"]),
    },
    ["results_jsonl"] = new JsonObject
    {
        ["lines"] = rows.Count, ["distinct_urls"] = urls.Count, ["bytes"] = jsonl.Length,
        ["status_200"] = rows.Count(r => Num(r["status"]) == 200),
        ["statuses"] = new JsonObject(rows.GroupBy(r => r["status"]?.ToJsonString() ?? "null").OrderBy(g => g.Key, StringComparer.Ordinal).Select(g => KeyValuePair.Create(g.Key, (JsonNode?)g.Count()))),
        ["errors"] = new JsonArray(rows.Where(r => r["error"] != null).Select(r =>
        {
            var msg = r["error"] is JsonValue ev && ev.TryGetValue<string>(out var es) ? es : r["error"]!.ToJsonString();
            return (JsonNode?)new JsonObject { ["url"] = Str(r["url"]), ["status"] = Copy(r["status"]), ["error"] = msg[..Math.Min(120, msg.Length)] };
        }).ToArray()),
        ["with_markdown"] = rows.Count(r => !string.IsNullOrEmpty(Str(r["markdown"]))),
        ["books_with_price"] = rows.Count(r => Str(r["extract"]?["price"]) != null),
        ["category_pages"] = rows.Count(r => Str(r["url"])!.Contains("/catalogue/category/")),
        ["depths"] = depths,
        ["first_run"] = firstRun.Count, ["second_run_new"] = urls.Count(u => !firstRun.Contains(u)),
        ["sample"] = new JsonArray(rows.Where(r => Str(r["extract"]?["price"]) != null).Take(2)
            .Select(r => (JsonNode?)new JsonObject { ["url"] = Str(r["url"]), ["title"] = Str(r["extract"]!["title"]), ["price"] = Str(r["extract"]!["price"]) }).ToArray()),
    },
    ["archive"] = new JsonObject
    {
        ["status"] = Str(archive["status"]), ["bytes"] = Copy(archive["bytes"]), ["downloaded_bytes"] = zipBytes.Length, ["files"] = names.Count,
        ["markdown_files"] = names.Count(x => x.EndsWith("/index.md")), ["extract_files"] = names.Count(x => x.EndsWith("/extract.json")),
        ["index_lines"] = indexLines,
        ["has"] = new JsonArray(new[] { "index.jsonl", "errors.jsonl", "job.json" }.Where(names.Contains).Select(x => (JsonNode?)x).ToArray()),
    },
    ["webhook"] = new JsonObject
    {
        ["deliveries"] = deliveries,
        ["final_done"] = finalData == null ? null : new JsonObject
        {
            ["data"] = finalData, ["signed_seconds_after_end"] = finalSignedAt - DateTimeOffset.Parse(Str(done["ended_at"])!).ToUnixTimeSeconds(),
        },
        ["archive"] = archiveData == null ? null : new JsonObject
        {
            ["status"] = Str(archiveData["status"]), ["bytes"] = Copy(archiveData["bytes"]), ["has_url"] = hookZipUrl != null,
            ["url_downloads_bytes"] = hookZipBytes,
            ["signed_seconds_after_end"] = archiveSignedAt - DateTimeOffset.Parse(Str(done["ended_at"])!).ToUnixTimeSeconds(),
        },
    },
    ["cost"] = new JsonObject
    {
        ["billed_thread_seconds"] = billed, ["pages"] = rows.Count, ["run_seconds"] = Round1(runSeconds),
        ["pages_per_minute"] = Round1(rows.Count / (runSeconds / 60)),
        ["thread_seconds_per_page"] = Round1(billed / rows.Count),
        ["client_running_seconds"] = Round1((tStopped - tStart + (tDone - tResume)).TotalSeconds),
    },
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
}.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = System.Text.Encodings.Web.JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
