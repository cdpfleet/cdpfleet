// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
var proxy = Environment.GetEnvironmentVariable("PROXY_URL")!;

// Six ways to drive a remote browser. Chromium (Playwright's build) and Patchright share a
// Chromium base, so they're the like-for-like pair; Chrome is also driven over CDP, with and
// without a console listener; Camoufox is Firefox, driven over Playwright's Juggler, not CDP.
var setups = new (string Label, string Engine, string Family, bool Cdp, bool Listen)[]
{
    ("Chromium, Playwright connect", "chromium", "chromium", false, true),
    ("Patchright, Playwright connect", "patchright", "chromium", false, true),
    ("Chrome, Playwright connect", "chrome", "chromium", false, true),
    ("Chrome, connectOverCDP", "chrome", "chromium", true, true),
    ("Chrome, connectOverCDP, no console listener", "chrome", "chromium", true, false),
    ("Camoufox, Playwright connect", "camoufox", "firefox", false, true),
};

// What a detection script can do from inside the page, run as the page's own script:
//  - the classic tell: log an Error whose `stack` is a getter; a debugger client that previews
//    logged objects used to read it;
//  - an Error whose `message` is a getter, the same idea;
//  - the cost of console.log: with a client listening to the console, every logged object is
//    serialized for it. Logging a big object 40 times is timed against JSON.stringify of the
//    same object (so the machine's speed cancels out), five times; the median ratio is kept.
// The verdict is left in an attribute that every client can read.
const string Page = """
    <html><body><p>console probe</p><script>
      const out = { stack_getter_read: false, message_getter_read: false };
      const e1 = new Error('probe');
      Object.defineProperty(e1, 'stack', { get() { out.stack_getter_read = true; return ''; } });
      console.debug(e1);
      const e2 = new Error('probe');
      Object.defineProperty(e2, 'message', { get() { out.message_getter_read = true; return ''; } });
      console.log(e2);
      console.log('hello-from-page');
      const big = Array.from({ length: 1000 }, (_, i) => ({ i, s: 'x'.repeat(40), a: [1, 2, 3, { d: i }] }));
      const ratios = [];
      for (let k = 0; k < 5; k++) {
        let t = performance.now();
        for (let i = 0; i < 40; i++) console.log(big);
        const logMs = performance.now() - t;
        t = performance.now();
        for (let i = 0; i < 40; i++) JSON.stringify(big);
        ratios.push(logMs / (performance.now() - t));
      }
      ratios.sort((a, b) => a - b);
      out.log_cost_ratio = Math.round(ratios[2] * 100) / 100;
      setTimeout(() => document.documentElement.setAttribute('data-out', JSON.stringify(out)), 1000);
    </script></body></html>
    """;

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

async Task<JsonObject> Run(string label, string engine, string family, bool cdp, bool listen)
{
    var body = new JsonObject { ["proxy"] = proxy, ["headless"] = true, ["record"] = false };
    if (cdp) body["cdp"] = true;
    var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
        new StringContent(body.ToJsonString(), System.Text.Encoding.UTF8, "application/json"));
    if (!res.IsSuccessStatusCode) return new JsonObject { ["label"] = label, ["error"] = $"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}" };
    var session = await res.Content.ReadFromJsonAsync<JsonElement>();
    var type = family == "firefox" ? playwright.Firefox : playwright.Chromium;
    var headers = new Dictionary<string, string> { ["x-api-key"] = key };
    var browser = cdp
        ? await type.ConnectOverCDPAsync(session.GetProperty("cdpUrl").GetString()!, new() { Headers = headers })
        : await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = headers });
    try
    {
        var context = cdp ? browser.Contexts[0] : await browser.NewContextAsync();
        var page = await context.NewPageAsync();
        var events = 0;
        if (listen) page.Console += (_, m) => { if (m.Text.Contains("hello-from-page")) events++; };
        await page.SetContentAsync(Page);
        await page.WaitForSelectorAsync("html[data-out]", new() { Timeout = 30000 });
        var probe = JsonNode.Parse(await page.EvaluateAsync<string>("() => document.documentElement.getAttribute('data-out')"))!.AsObject();
        var outp = new JsonObject
        {
            ["label"] = label, ["engine"] = engine, ["connect"] = cdp ? "connectOverCDP" : "connect",
            ["console_listener"] = listen, ["console_events"] = listen ? events : (int?)null,
        };
        foreach (var (k, v) in probe) outp[k] = v?.DeepClone();
        outp["evaluate_works"] = await page.EvaluateAsync<int>("() => 6 * 7") == 42;
        return outp;
    }
    finally
    {
        await browser.CloseAsync();
    }
}

var rows = new JsonArray();
foreach (var s in setups) rows.Add(await Run(s.Label, s.Engine, s.Family, s.Cdp, s.Listen)); // one at a time: an idle session ends after 2 minutes by default
Console.WriteLine(new JsonObject { ["setups"] = rows }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
