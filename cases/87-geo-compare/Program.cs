// .NET 8+, no packages. env: CDPFLEET_API_KEY (five countries in one call hold five threads: a paid plan)
using System.Diagnostics;
using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;

var KEY = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string API = "https://cdpfleet.com";
string[] COUNTRIES = ["us", "de", "gb", "jp", "br"]; // the first one is the baseline the diff compares with
string[] FORMATS = ["markdown", "html"];
const int MAX_WAIT = 45;
(string Name, string Url)[] PAGES = [
    ("Steam store page", "https://store.steampowered.com/app/620/Portal_2/"),
    ("Google home page", "https://www.google.com/"),
    ("Nike home page", "https://www.nike.com/"),
    ("example.com (control)", "https://example.com/"),
];
// Prices in the five countries' currencies: R$ before $, the symbol before or after the number.
// Plain [0-9] and [ \u00a0] rather than \d and \s, so the pattern means the same in all five languages.
var PRICE = new Regex(@"R\$[ \u00a0]?[0-9][0-9.,]*|US\$[ \u00a0]?[0-9][0-9.,]*|£[ \u00a0]?[0-9][0-9.,]*|€[ \u00a0]?[0-9][0-9.,]*"
    + @"|[0-9][0-9.,]*[ \u00a0]?€|[¥￥][ \u00a0]?[0-9][0-9,]*|[0-9][0-9,]*[ \u00a0]?円|\$[ \u00a0]?[0-9][0-9.,]*");
// The "accept all" button of a cookie banner, in the five countries' languages.
var CONSENT = new Regex("Accept all|Alle akzeptieren|Aceitar todos|Aceitar tudo|すべて同意|すべて受け入れる|すべて許可", RegexOptions.IgnoreCase);
var since = DateTime.UtcNow.AddSeconds(-60).ToString("o");
var http = new HttpClient { Timeout = TimeSpan.FromSeconds(MAX_WAIT + 30) };
http.DefaultRequestHeaders.Add("x-api-key", KEY);

int Bytes(JsonNode? n) => n is JsonValue v && v.TryGetValue<string>(out var s) ? Encoding.UTF8.GetByteCount(s) : 0;
// A diff's size, or null when the diff is null (this country or the baseline failed).
int? DiffBytes(JsonNode? n) => n == null ? null : Bytes(n);
string? Str(JsonNode? o, string k) => o?[k] is JsonValue v && v.TryGetValue<string>(out var s) ? s : null;
List<string> Prices(string text) => PRICE.Matches(text).Select(m => Regex.Replace(m.Value, "[.,]$", "").Replace('\u00a0', ' ')).ToList();
string Currency(string p) => p.StartsWith("R$") ? "BRL" : p.Contains('£') ? "GBP" : p.Contains('€') ? "EUR" : Regex.IsMatch(p, "[¥￥円]") ? "JPY" : "USD";
string? Attr(string html, string pattern) { var m = Regex.Match(html, pattern, RegexOptions.IgnoreCase); return m.Success ? m.Groups[1].Value : null; }
JsonArray Arr(IEnumerable<string> xs) => new(xs.Select(x => (JsonNode)JsonValue.Create(x)!).ToArray());
bool Loaded(JsonNode? r) => r?["status"] is JsonValue v && v.TryGetValue<int>(out var s) && s >= 200 && s < 400;

// What the Function's unified diff (one country against the baseline) says.
JsonNode? ReadDiff(JsonNode? d)
{
    if (d is not JsonValue v || !v.TryGetValue<string>(out var s)) return null;
    var lines = s.Split('\n');
    var added = lines.Where(l => l.StartsWith('+') && !l.StartsWith("+++")).ToList();
    var removed = lines.Where(l => l.StartsWith('-') && !l.StartsWith("---")).ToList();
    var was = removed.SelectMany(Prices).ToList();
    var now = added.SelectMany(Prices).ToList();
    var changes = new List<string>();
    for (var i = 0; i < Math.Min(was.Count, now.Count) && changes.Count < 3; i++) if (was[i] != now[i]) changes.Add($"{was[i]} → {now[i]}");
    return new JsonObject { ["bytes"] = Bytes(d), ["hunks"] = lines.Count(l => l.StartsWith("@@")), ["added"] = added.Count, ["removed"] = removed.Count, ["price_changes"] = Arr(changes) };
}

// The call in the account's history (its x-call-id is the session id there): weight and billed thread-seconds.
async Task<JsonNode?> Billed(string? id)
{
    for (var i = 0; i < 30 && id != null; i++)
    {
        var r = JsonNode.Parse(await http.GetStringAsync($"{API}/v1/me/sessions?limit=200&from={Uri.EscapeDataString(since)}"));
        var s = r?["sessions"]?.AsArray().FirstOrDefault(x => Str(x, "session_id") == id);
        if (Str(s, "ended_at") is string ended)
        {
            var weight = (int)s!["weight"]!;
            var ms = (DateTimeOffset.Parse(ended) - DateTimeOffset.Parse(Str(s, "started_at")!)).TotalMilliseconds;
            return new JsonObject { ["engine"] = Str(s, "engine"), ["weight"] = weight, ["thread_seconds"] = Math.Round(ms * weight / 100) / 10 };
        }
        await Task.Delay(2000);
    }
    return null;
}

async Task<(HttpResponseMessage Res, byte[] Raw, JsonObject Body, long Ms)> Geo(string url, string[]? countries = null, int maxWait = MAX_WAIT, string[]? formats = null)
{
    var sw = Stopwatch.StartNew();
    var payload = new JsonObject { ["url"] = url, ["countries"] = Arr(countries ?? COUNTRIES), ["formats"] = Arr(formats ?? FORMATS), ["max_wait"] = maxWait };
    var res = await http.PostAsync($"{API}/v1/functions/geo", new StringContent(payload.ToJsonString(), Encoding.UTF8, "application/json"));
    var raw = await res.Content.ReadAsByteArrayAsync();
    JsonObject body;
    try { body = JsonNode.Parse(raw) as JsonObject ?? new JsonObject(); } catch (JsonException) { body = new JsonObject(); }
    return (res, raw, body, sw.ElapsedMilliseconds);
}
string? Header(HttpResponseMessage r, string name) => r.Headers.TryGetValues(name, out var v) ? v.First() : null;

var pages = new JsonArray();
var ids = new List<(string? Call, string? First)>();
foreach (var (name, url) in PAGES) // one call per page, one after another
{
    var first = await Geo(url);
    var call = first;
    // Every diff is against the first country: if that one failed, the call is retried once.
    if ((int)call.Res.StatusCode == 200 && !Loaded(call.Body["results"]?[COUNTRIES[0]])) call = await Geo(url);
    var retried = !ReferenceEquals(call.Body, first.Body);
    var results = call.Body["results"];
    var diff = call.Body["diff"];
    var baseMd = Str(results?[COUNTRIES[0]], "markdown");
    var countries = new JsonObject();
    foreach (var c in COUNTRIES)
    {
        var r = results?[c];
        var html = Str(r, "html") ?? "";
        var md = Str(r, "markdown") ?? "";
        var found = Prices(md);
        countries[c] = new JsonObject
        {
            ["status"] = r?["status"]?.DeepClone(),
            ["error"] = Str(r, "error"),
            ["partial"] = r?["partial"] is JsonValue p && p.TryGetValue<bool>(out var pb) && pb,
            ["lang"] = Attr(html, "<html[^>]*\\slang=\"([^\"]+)\""),
            ["url"] = Str(r, "url"), // the final URL after redirects (only for a country that loaded)
            ["title"] = Attr(html, "<title[^>]*>([^<]*)<")?.Trim(),
            ["currencies"] = Arr(found.Select(Currency).Distinct()),
            ["prices"] = Arr(found.Take(4)),
            ["consent_banner"] = html.Length == 0 ? null : CONSENT.IsMatch(html),
            ["markdown_bytes"] = Encoding.UTF8.GetByteCount(md),
            ["html_bytes"] = Encoding.UTF8.GetByteCount(html),
            ["same_markdown_as_baseline"] = c == COUNTRIES[0] || md.Length == 0 || string.IsNullOrEmpty(baseMd) ? null : md == baseMd,
            // A diff is a string ("" = identical), or null when this country or the baseline failed.
            ["diff"] = c == COUNTRIES[0] ? null : new JsonObject { ["markdown"] = ReadDiff(diff?["markdown"]?[c]), ["html_bytes"] = DiffBytes(diff?["html"]?[c]) },
        };
    }
    JsonObject? firstAttempt = null;
    if (retried)
    {
        var errors = new JsonObject();
        foreach (var c in COUNTRIES) if (!Loaded(first.Body["results"]?[c])) errors[c] = Str(first.Body["results"]?[c], "error");
        var diffs = new JsonObject();
        foreach (var (c, d) in first.Body["diff"]?["markdown"]?.AsObject() ?? new JsonObject()) diffs[c] = DiffBytes(d);
        firstAttempt = new JsonObject { ["call_id"] = Header(first.Res, "x-call-id"), ["errors"] = errors, ["diffs"] = diffs };
    }
    var diffBytes = new JsonObject();
    foreach (var f in new[] { "markdown", "html" }) diffBytes[f] = diff?[f]?.AsObject().Sum(e => Bytes(e.Value)) ?? 0;
    pages.Add(new JsonObject
    {
        ["page"] = name,
        ["url"] = url,
        ["attempts"] = retried ? 2 : 1,
        ["first_attempt"] = firstAttempt,
        ["status"] = (int)call.Res.StatusCode,
        ["error"] = Str(call.Body, "error"),
        ["seconds"] = Math.Round(call.Ms / 100.0) / 10,
        ["call_id"] = Header(call.Res, "x-call-id"),
        ["warning"] = Header(call.Res, "x-cdpfleet-warning"),
        ["response_bytes"] = call.Raw.Length,
        ["diff_bytes"] = diffBytes,
        ["ok_countries"] = Arr(COUNTRIES.Where(c => Loaded(results?[c]))),
        ["countries"] = countries,
    });
    ids.Add((Header(call.Res, "x-call-id"), retried ? Header(first.Res, "x-call-id") : null));
}
// What a failure looks like: the same Steam page with max_wait 1, so no country can load it in time.
var failed = await Geo("https://store.steampowered.com/app/620/Portal_2/", new[] { "us", "de" }, 1, new[] { "markdown" });
var failedResults = new JsonObject();
foreach (var (c, r) in failed.Body["results"]?.AsObject() ?? new JsonObject())
    failedResults[c] = new JsonObject { ["status"] = r?["status"]?.DeepClone(), ["error"] = Str(r, "error") };
var failure = new JsonObject
{
    ["status"] = (int)failed.Res.StatusCode, ["max_wait"] = 1, ["results"] = failedResults,
    ["diff"] = failed.Body["diff"]?["markdown"]?.DeepClone(),
};

for (var i = 0; i < pages.Count; i++)
{
    pages[i]!["billed"] = await Billed(ids[i].Call);
    if (ids[i].First != null) pages[i]!["first_attempt"]!["billed"] = await Billed(ids[i].First);
}

var output = new JsonObject
{
    ["language"] = "csharp",
    ["countries"] = Arr(COUNTRIES),
    ["formats"] = Arr(FORMATS),
    ["max_wait"] = MAX_WAIT,
    ["pages"] = pages,
    ["failure"] = failure,
    ["measured_at"] = DateTime.UtcNow.ToString("yyyy-MM-dd"),
};
Console.OutputEncoding = Encoding.UTF8;
Console.WriteLine(output.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
