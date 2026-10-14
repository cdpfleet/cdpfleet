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
var setups = new (string Label, string Engine, string Family, bool Headless)[]
{
    ("Chromium, headless", "chromium", "chromium", true),
    ("Chrome, headful", "chrome", "chromium", false),
    ("Firefox, headful", "firefox", "firefox", false),
    ("WebKit, headful", "webkit", "webkit", false),
};

// A made-up https origin served by Playwright's router: a secure context (the clipboard and
// file APIs need one) with no network traffic. The page has a file input and a button that
// opens the File System Access picker; both report what they received in data attributes.
const string PageUrl = "https://app.example.test/";
const string Html = """
<html><body>
<input type="file" id="f" onchange="document.body.dataset.input = this.files[0].name + ':' + this.files[0].size">
<button id="b" onclick="showOpenFilePicker().then(async ([h]) => { const f = await h.getFile(); document.body.dataset.picked = f.name + ':' + f.size; }).catch((e) => { document.body.dataset.picked = 'error: ' + e.name; })">open</button>
</body></html>
""";

// Clipboard round trip inside the page; a permission prompt nobody answers would hang, so 3 s cap.
const string Clipboard = """
(async () => {
  const cap = (p) => Promise.race([p, new Promise((_, rej) => setTimeout(() => rej(new Error('timed out (prompt?)')), 3000))]);
  const out = {};
  try { await cap(navigator.clipboard.writeText('copied-' + Date.now())); out.write = 'ok'; } catch (e) { out.write = e.name === 'Error' ? e.message : e.name; }
  try { const t = await cap(navigator.clipboard.readText()); out.read = t.startsWith('copied-') ? 'ok' : 'empty'; } catch (e) { out.read = e.name === 'Error' ? e.message : e.name; }
  return out;
})()
""";

// The origin-private file system: a sandboxed folder per site, no picker, no permission.
const string Opfs = """
(async () => {
  try {
    const root = await navigator.storage.getDirectory();
    const fh = await root.getFileHandle('note.txt', { create: true });
    const w = await fh.createWritable(); await w.write('hello opfs'); await w.close();
    return (await (await fh.getFile()).text()) === 'hello opfs' ? 'ok' : 'mismatch';
  } catch (e) { return 'no (' + e.name + ')'; }
})()
""";

// Stands in for the native picker: returns a handle to a file the script provides.
const string PickerStub = """
window.showOpenFilePicker = async () => [{
  kind: 'file', name: 'report.csv', getFile: async () => new File(['id,total\n1,42\n'], 'report.csv', { type: 'text/csv' }),
}];
""";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
using var playwright = await Playwright.CreateAsync();

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(string engine, JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync($"https://starter.cdpfleet.com/{engine}/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {engine} {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

Task Serve(IBrowserContext context) => context.RouteAsync(PageUrl + "**", route => route.FulfillAsync(new() { ContentType = "text/html", Body = Html }));

async Task<IPage> FreshPage(IBrowser browser, string? initScript = null)
{
    var context = await browser.NewContextAsync();
    await Serve(context);
    if (initScript != null) await context.AddInitScriptAsync(initScript);
    var page = await context.NewPageAsync();
    await page.GotoAsync(PageUrl);
    return page;
}

async Task<IFileChooser?> ChooserAfterClick(IPage page, string selector)
{
    try { return await page.RunAndWaitForFileChooserAsync(() => page.ClickAsync(selector), new() { Timeout = 3000 }); }
    catch (TimeoutException) { return null; }
}

async Task<JsonObject> One(string label, string engine, string family, bool headless)
{
    var session = await Launch(engine, new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = headless });
    var type = family switch { "firefox" => playwright.Firefox, "webkit" => playwright.Webkit, _ => playwright.Chromium };
    var browser = await type.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
    var outp = new JsonObject { ["label"] = label };
    try
    {
        // 1. Clipboard with no permissions granted.
        var page = await FreshPage(browser);
        var d = await page.EvaluateAsync<JsonElement>(Clipboard);
        outp["clipboard_write_default"] = d.GetProperty("write").GetString();
        outp["clipboard_read_default"] = d.GetProperty("read").GetString();

        // 2. Files in the same page: <input type=file>, the File System Access picker, the private file system.
        var fc = await ChooserAfterClick(page, "#f");
        if (fc != null) await fc.SetFilesAsync(new FilePayload { Name = "report.csv", MimeType = "text/csv", Buffer = Encoding.UTF8.GetBytes("id,total\n1,42\n") });
        outp["input_set_files"] = fc != null ? await page.EvaluateAsync<string>("() => document.body.dataset.input || 'nothing'") : "no filechooser event";
        var hasPicker = await page.EvaluateAsync<bool>("() => typeof window.showOpenFilePicker === 'function'");
        outp["show_open_file_picker"] = hasPicker;
        outp["picker_filechooser_event"] = hasPicker && await ChooserAfterClick(page, "#b") != null;
        outp["private_file_system"] = await page.EvaluateAsync<string>(Opfs);
        await page.Context.CloseAsync();

        // 3. The picker stubbed with an init script: the page's own code receives our file.
        page = await FreshPage(browser, PickerStub);
        await page.ClickAsync("#b");
        await page.WaitForFunctionAsync("() => document.body.dataset.picked", null, new() { Timeout = 5000 });
        outp["picker_stubbed"] = await page.EvaluateAsync<string>("() => document.body.dataset.picked");
        await page.Context.CloseAsync();

        // 4. Clipboard with Chromium's permission names granted. Some engines reject the names —
        // at GrantPermissions or when the next page opens.
        var context = await browser.NewContextAsync();
        await Serve(context);
        try
        {
            await context.GrantPermissionsAsync(new[] { "clipboard-read", "clipboard-write" }, new() { Origin = PageUrl });
            page = await context.NewPageAsync();
            await page.GotoAsync(PageUrl);
            outp["grant_clipboard"] = "accepted";
            var g = await page.EvaluateAsync<JsonElement>(Clipboard);
            outp["clipboard_write_granted"] = g.GetProperty("write").GetString();
            outp["clipboard_read_granted"] = g.GetProperty("read").GetString();
        }
        catch (PlaywrightException err)
        {
            var msg = Regex.Replace(err.Message.Split('\n')[0], @"^[A-Za-z]+\.[A-Za-z]+: ", "");
            outp["grant_clipboard"] = msg[..Math.Min(80, msg.Length)];
        }
    }
    finally
    {
        await browser.CloseAsync();
    }
    return outp;
}

var results = new JsonArray();
foreach (var s in setups) results.Add(await One(s.Label, s.Engine, s.Family, s.Headless));
Console.WriteLine(new JsonObject { ["results"] = results }.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
