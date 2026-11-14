// .NET 9, Microsoft.Playwright 1.60.0
// env: CDPFLEET_API_KEY (a paid plan: jobs and profiles aren't on Community), PROXY_URL_US (a sticky US exit)
using System.Security.Cryptography;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL_US")!;
const string Api = "https://cdpfleet.com";
var name = $"case90-csharp-{Convert.ToHexString(RandomNumberGenerator.GetBytes(4)).ToLowerInvariant()}"; // a profile of its own for each run
// Three ordinary public sites: an encyclopedia, a scraping sandbox, a news front page.
string[] sites = ["https://en.wikipedia.org/wiki/Main_Page", "https://books.toscrape.com/", "https://www.theguardian.com/us"];
var warmup = new JsonObject
{
    ["url"] = sites[0],
    ["sites"] = new JsonArray(sites.Skip(1).Select(s => (JsonNode)s).ToArray()),
    ["browser"] = "camoufox",
    ["profile"] = name,
    ["duration"] = 600, // a safety net: the program stops the job itself
    // No typing: the warmup would otherwise type into search boxes now and then.
    ["behavior"] = new JsonObject { ["scroll"] = true, ["mouse"] = true, ["typing"] = false, ["dwell_s"] = new JsonArray(4, 10), ["pages_per_site"] = new JsonArray(2, 4) },
};
const int StopAfterS = 150;
string[] ended = ["done", "failed", "stopped", "interrupted"];
const double Day = 86400;
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(120) };

async Task<(int Status, JsonObject Json, string Text)> CallApi(HttpMethod method, string path, JsonNode? body = null)
{
    var req = new HttpRequestMessage(method, Api + path);
    req.Headers.Add("x-api-key", key);
    if (body != null) req.Content = new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json");
    var res = await http.SendAsync(req);
    var text = await res.Content.ReadAsStringAsync();
    JsonObject json;
    try { json = JsonNode.Parse(text)!.AsObject(); } catch { json = new JsonObject(); }
    return ((int)res.StatusCode, json, text);
}
string? Str(JsonNode? n) => n is JsonValue v && v.TryGetValue<string>(out var s) ? s : null;
double Num(JsonNode? n) => n is JsonValue v && v.TryGetValue<double>(out var d) ? d : 0;
double Secs(double ms) => Math.Round(ms / 100, MidpointRounding.AwayFromZero) / 10;
double Ms(JsonNode? iso) => DateTimeOffset.Parse(Str(iso)!).ToUnixTimeMilliseconds(); // an API timestamp
double NowMs() => DateTimeOffset.UtcNow.ToUnixTimeMilliseconds();
JsonNode? Copy(JsonNode? n) => n?.DeepClone();

async Task<JsonObject?> ProfileInfo()
{
    var r = (await CallApi(HttpMethod.Get, "/v1/me/profiles")).Json["profiles"]?.AsArray();
    return r?.FirstOrDefault(p => Str(p!["name"]) == name)?.AsObject();
}

// The site a cookie or a host belongs to: its last two labels (three under co.uk and the like).
string Site(string host)
{
    var l = host.TrimStart('.').Split('.');
    var n = l.Length >= 2 && Regex.IsMatch($"{l[^2]}.{l[^1]}", "^(co|com|org|net|ac|gov)\\.[a-z]{2}$") ? 3 : 2;
    return string.Join(".", l.Skip(Math.Max(0, l.Length - n)));
}

// Cookies by site, first- vs third-party (first = a site the warmup visited as a page), and how long they last.
JsonObject Summarize(List<Ck> cookies, List<string> firstParty, double now)
{
    var bySite = cookies.GroupBy(c => Site(c.Domain)).ToDictionary(g => g.Key, g => g.Count());
    var third = bySite.Where(e => !firstParty.Contains(e.Key)).OrderBy(e => -e.Value).ThenBy(e => e.Key, StringComparer.Ordinal).ToList();
    var left = cookies.Where(c => c.Expires > 0).Select(c => (c.Expires - now) / Day).ToList();
    var first = cookies.Count(c => firstParty.Contains(Site(c.Domain)));
    var byFirst = new JsonObject();
    foreach (var s in firstParty) byFirst[s] = bySite.GetValueOrDefault(s, 0);
    return new JsonObject
    {
        ["total"] = cookies.Count,
        ["sites"] = bySite.Count,
        ["first_party"] = first,
        ["third_party"] = cookies.Count - first,
        ["third_party_sites"] = third.Count,
        ["by_first_party_site"] = byFirst,
        ["top_third_party"] = new JsonArray(third.Take(5).Select(e => (JsonNode)$"{e.Key} {e.Value}").ToArray()),
        ["session"] = cookies.Count - left.Count,
        ["http_only"] = cookies.Count(c => c.HttpOnly),
        ["same_site_none"] = cookies.Count(c => c.SameSite == "None"),
        ["expiry"] = new JsonObject
        {
            ["under_1_day"] = left.Count(d => d < 1),
            ["under_30_days"] = left.Count(d => d >= 1 && d < 30),
            ["under_1_year"] = left.Count(d => d >= 30 && d < 365),
            ["a_year_or_more"] = left.Count(d => d >= 365),
            ["max_days"] = left.Count > 0 ? (JsonNode)Math.Round(left.Max(), MidpointRounding.AwayFromZero) : null,
        },
        ["same_key_twice"] = cookies.Count - cookies.Select(c => c.Key).Distinct().Count(), // e.g. Firefox's per-site (partitioned) copies
    };
}

async Task<(string SessionId, IBrowser Browser)> Launch(IPlaywright pw, JsonNode? profile)
{
    var body = new JsonObject { ["proxy"] = proxy, ["headless"] = true };
    if (profile != null) body["profile"] = profile.DeepClone();
    for (var attempt = 1; ; attempt++)
    {
        var req = new HttpRequestMessage(HttpMethod.Post, "https://starter.cdpfleet.com/camoufox/session");
        req.Headers.Add("x-api-key", key);
        req.Content = new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json");
        var res = await http.SendAsync(req);
        var text = await res.Content.ReadAsStringAsync();
        if (res.IsSuccessStatusCode)
        {
            var s = JsonNode.Parse(text)!;
            var browser = await pw.Firefox.ConnectAsync(Str(s["wsUrl"])!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
            return (Str(s["sessionId"])!, browser);
        }
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {text}");
        await Task.Delay((int.TryParse(res.Headers.TryGetValues("retry-after", out var v) ? v.First() : "5", out var ra) ? ra : 5) * 1000);
    }
}

// What one later session sees: the cookies it starts with, then for each warmed site one request for its
// /robots.txt — the Cookie header that first request carries is what the site gets from this browser, and
// the plain-text page lets us read the origin's localStorage and IndexedDB with none of the site's scripts run.
async Task<(string SessionId, List<Ck> CookiesAtStart, JsonArray Origins, double Seconds)> Inspect(IPlaywright pw, JsonNode? profile)
{
    var (sessionId, browser) = await Launch(pw, profile);
    var t = NowMs();
    try
    {
        // With a profile, its context is browser.Contexts[0]; without one, a fresh context.
        var context = profile != null ? browser.Contexts[0] : await browser.NewContextAsync();
        var page = context.Pages.Count > 0 ? context.Pages[0] : await context.NewPageAsync();
        var atStart = (await context.CookiesAsync()).Select(c => new Ck(c.Name, c.Domain, c.Path, c.Expires, c.HttpOnly, c.SameSite.ToString())).ToList();
        var origins = new JsonArray();
        foreach (var url in sites)
        {
            var u = new Uri(url);
            var origin = $"{u.Scheme}://{u.Host}";
            IResponse? res = null;
            for (var attempt = 1; attempt <= 3 && res == null; attempt++)
            {
                try { res = await page.GotoAsync($"{origin}/robots.txt", new() { Timeout = 60000 }); }
                catch (PlaywrightException) { if (attempt == 3) throw; await Task.Delay(2000); }
            }
            var headers = await res!.Request.AllHeadersAsync();
            var header = headers.TryGetValue("cookie", out var h) ? h : "";
            var names = header.Length > 0 ? header.Split("; ").Select(c => c.Contains('=') ? c[..c.IndexOf('=')] : c).ToList() : [];
            var storage = await page.EvaluateAsync<JsonElement>("async () => ({ local_storage: localStorage.length, indexeddb: (await indexedDB.databases()).length })");
            origins.Add(new JsonObject
            {
                ["origin"] = origin,
                ["status"] = res.Status,
                ["cookie_header_cookies"] = names.Count,
                ["cookie_header_names"] = new JsonArray(names.Take(8).Select(n => (JsonNode)n).ToArray()),
                ["local_storage"] = storage.GetProperty("local_storage").GetInt32(),
                ["indexeddb"] = storage.GetProperty("indexeddb").GetInt32(),
            });
            await Task.Delay(3000);
        }
        return (sessionId, atStart, origins, Secs(NowMs() - t));
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var shown = (JsonObject)warmup.DeepClone();
shown["profile"] = "case90-csharp-…";
shown["stop_after_s"] = StopAfterS;
var output = new JsonObject
{
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
    ["sites"] = new JsonArray(sites.Select(s => (JsonNode)s).ToArray()),
    ["warmup"] = shown,
};
string? id = null;
try
{
    // 1. Start the warmup into a new profile, on a sticky US exit.
    var t0 = NowMs();
    var body = (JsonObject)warmup.DeepClone();
    body["launch"] = new JsonObject { ["headless"] = true, ["proxy"] = proxy };
    var start = await CallApi(HttpMethod.Post, "/v1/functions/warmup", body);
    if (start.Status != 202) throw new Exception($"warmup {start.Status} {start.Text}");
    id = Str(start.Json["job_id"])!;
    shown["start_status"] = start.Status;

    // 2. Follow its progress; stop it after StopAfterS seconds.
    var samples = new List<JsonObject>();
    JsonObject job;
    JsonObject? stop = null;
    double stopAt = 0;
    while (true)
    {
        await Task.Delay(5000);
        job = (await CallApi(HttpMethod.Get, $"/v1/jobs/{id}")).Json;
        var p = job["progress"] is JsonObject po ? (JsonObject)po.DeepClone() : new JsonObject();
        p["s"] = (long)Math.Round((NowMs() - t0) / 1000);
        samples.Add(p);
        if (ended.Contains(Str(job["status"]))) break;
        if (stop == null && NowMs() - t0 >= StopAfterS * 1000)
        {
            var r = await CallApi(HttpMethod.Delete, $"/v1/jobs/{id}");
            stop = new JsonObject { ["after_seconds"] = Secs(NowMs() - t0), ["status"] = r.Status, ["reply"] = Str(r.Json["status"]) };
            stopAt = NowMs();
        }
        if (NowMs() - t0 > (StopAfterS + 120) * 1000) throw new Exception($"job still {Str(job["status"])}");
    }
    var endedAt = Ms(job["ended_at"]);
    if (stop != null) stop["seconds_to_ended"] = Secs(endedAt - stopAt);
    var kept = new JsonArray();
    for (var i = 0; i < samples.Count; i++)
    {
        var p = samples[i];
        if (p["visits"] == null) continue;
        if (i < samples.Count - 1 && Num(p["visits"]) == Num(samples[i + 1]["visits"])) continue;
        kept.Add(new JsonObject { ["s"] = Copy(p["s"]), ["visits"] = Copy(p["visits"]), ["sites"] = Copy(p["sites"]), ["challenges"] = Copy(p["challenges"]) });
    }
    output["job"] = new JsonObject
    {
        ["status"] = Str(job["status"]), ["own_session"] = Copy(job["own_session"]), ["runs"] = Copy(job["runs"]), ["progress"] = Copy(job["progress"]), ["error"] = Copy(job["error"]),
        ["billed_seconds"] = Copy(job["billed_seconds"]), ["run_seconds"] = Secs(endedAt - Ms(job["started_at"])),
        ["stop"] = stop, ["progress_samples"] = kept,
    };

    // 3. The visit history: the job's results log every page it browsed.
    var lines = new List<JsonNode>();
    for (JsonNode? cursor = 0; cursor != null;)
    {
        var r = (await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/results?cursor={cursor.ToJsonString()}")).Json;
        lines.AddRange(r["results"]!.AsArray().Select(x => x!.DeepClone()));
        var next = r["next_cursor"];
        if (next != null && next.ToJsonString() == cursor.ToJsonString()) break;
        cursor = next?.DeepClone();
    }
    var visits = lines.Where(l => Str(l["type"]) == "visit").ToList();
    var firstParty = visits.Select(v => Site(new Uri(Str(v["url"])!).Host)).Distinct().OrderBy(s => s, StringComparer.Ordinal).ToList();
    var bySiteV = new JsonObject();
    foreach (var s in firstParty) bySiteV[s] = visits.Count(v => Site(new Uri(Str(v["url"])!).Host) == s);
    output["visits"] = new JsonObject
    {
        ["count"] = visits.Count,
        ["challenged"] = visits.Count(v => v["challenged"] is JsonValue c && c.TryGetValue<bool>(out var b) && b),
        ["by_site"] = bySiteV,
        ["statuses"] = new JsonArray(visits.Select(v => (int)Num(v["status"])).Distinct().Order().Select(x => (JsonNode)x).ToArray()),
        ["dwell_s"] = visits.Count > 0 ? new JsonObject { ["min"] = visits.Min(v => Num(v["dwell_s"])), ["max"] = visits.Max(v => Num(v["dwell_s"])) } : null,
        ["paths"] = new JsonArray(visits.Select(v => { var u = new Uri(Str(v["url"])!); return (JsonNode)(u.Host + u.AbsolutePath); }).ToArray()),
        ["cookie_lines"] = lines.Count(l => Str(l["type"]) == "cookies"),
    };

    // 4. The cookies it left: GET /v1/jobs/{id}/cookies (after the end, the last set it delivered).
    var ck = await CallApi(HttpMethod.Get, $"/v1/jobs/{id}/cookies");
    var jobCookies = (ck.Json["cookies"]?.AsArray() ?? []).Select(c => new Ck(Str(c!["name"])!, Str(c["domain"])!, Str(c["path"])!,
        c["expires"] != null ? Num(c["expires"]) : -1, c["httpOnly"] is JsonValue hv && hv.TryGetValue<bool>(out var ho) && ho, Str(c["sameSite"]))).ToList();
    var jc = new JsonObject { ["status"] = ck.Status, ["live"] = Copy(ck.Json["live"]), ["at_vs_end_seconds"] = Secs(Ms(ck.Json["at"]) - endedAt) };
    foreach (var (k, v) in Summarize(jobCookies, firstParty, NowMs() / 1000)) jc[k] = v?.DeepClone();
    output["job_cookies"] = jc;

    // 5. The profile, saved after the job's browser exited.
    JsonObject? prof = null;
    for (var i = 0; i < 60; i++)
    {
        prof = await ProfileInfo();
        if (prof != null && Num(prof["version"]) >= 1 && prof["in_use"]?.GetValue<bool>() == false) break;
        await Task.Delay(2000);
    }
    output["profile"] = prof == null ? null : new JsonObject
    {
        ["engine"] = Copy(prof["engine"]), ["version"] = Copy(prof["version"]), ["bytes"] = Copy(prof["bytes"]),
        ["saved_after_end_seconds"] = Secs(Ms(prof["saved_at"]) - endedAt),
    };

    // 6. A later session on the profile (read-only: it sees the profile and doesn't change it), then a fresh one.
    using var pw = await Playwright.CreateAsync();
    var later = await Inspect(pw, new JsonObject { ["id"] = name, ["mode"] = "readonly" });
    var keys = later.CookiesAtStart.Select(c => c.Key).ToHashSet();
    var jobKeys = jobCookies.Select(c => c.Key).ToHashSet();
    var session = jobCookies.Where(c => !(c.Expires > 0)).ToList();
    output["later"] = new JsonObject
    {
        ["cookies_at_start"] = Summarize(later.CookiesAtStart, firstParty, NowMs() / 1000),
        ["job_cookies_restored"] = jobKeys.Count(keys.Contains),
        ["job_cookies_distinct"] = jobKeys.Count,
        ["session_cookies_restored"] = session.Count(c => keys.Contains(c.Key)),
        ["session_cookies_in_job"] = session.Count,
        ["not_in_job"] = keys.Count(k => !jobKeys.Contains(k)),
        ["origins"] = later.Origins,
        ["seconds"] = later.Seconds,
    };
    var fresh = await Inspect(pw, null);
    output["fresh"] = new JsonObject { ["cookies_at_start"] = fresh.CookiesAtStart.Count, ["origins"] = fresh.Origins, ["seconds"] = fresh.Seconds };

    // 7. The bill: the warmup's own session and the two later sessions, from the session history.
    var since = Uri.EscapeDataString(DateTimeOffset.FromUnixTimeMilliseconds((long)t0 - 60000).UtcDateTime.ToString("yyyy-MM-ddTHH:mm:ss.fffZ"));
    string?[] wanted = [Str(job["session_id"]), later.SessionId, fresh.SessionId];
    var sessions = new JsonArray();
    string? Sid(JsonNode s) => Str(s["session_id"]) ?? Str(s["id"]);
    for (var i = 0; i < 15; i++)
    {
        sessions = (await CallApi(HttpMethod.Get, $"/v1/me/sessions?from={since}&limit=200")).Json["sessions"]?.AsArray() ?? [];
        if (wanted.All(w => sessions.Any(x => w != null && Sid(x!) == w && Str(x!["ended_at"]) != null))) break;
        await Task.Delay(2000);
    }
    JsonNode? Bill(string? sid)
    {
        var s = sessions.FirstOrDefault(x => sid != null && Sid(x!) == sid);
        if (s == null) return null;
        return new JsonObject { ["engine"] = Copy(s["engine"]), ["weight"] = Copy(s["weight"]), ["seconds"] = Str(s["ended_at"]) != null ? Secs(Ms(s["ended_at"]) - Ms(s["started_at"])) : null };
    }
    output["bill"] = new JsonObject
    {
        ["warmup"] = new JsonObject { ["billed_seconds"] = Copy(job["billed_seconds"]), ["session"] = Bill(Str(job["session_id"])) },
        ["later"] = Bill(later.SessionId),
        ["fresh"] = Bill(fresh.SessionId),
    };
}
finally
{
    // Stop the job if something failed early, then delete the profile once nothing holds it.
    if (id != null && !output.ContainsKey("job")) await CallApi(HttpMethod.Delete, $"/v1/jobs/{id}");
    for (var i = 0; i < 60 && (await ProfileInfo())?["in_use"]?.GetValue<bool>() == true; i++) await Task.Delay(2000);
    var del = await CallApi(HttpMethod.Delete, $"/v1/me/profiles/{name}");
    output["cleanup"] = new JsonObject
    {
        ["delete_status"] = del.Status,
        ["deleted"] = del.Status == 200 && del.Json["deleted"]?.GetValue<bool>() == true && await ProfileInfo() == null,
    };
}
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));

// A cookie as the API and Playwright both describe it.
record Ck(string Name, string Domain, string Path, double Expires, bool HttpOnly, string? SameSite)
{
    public string Key => $"{Name}|{Domain}|{Path}";
}
