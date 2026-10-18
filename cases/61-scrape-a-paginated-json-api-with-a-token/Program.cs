// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text;
using System.Text.Json;
using System.Text.Json.Nodes;
using Microsoft.Playwright;

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
const string Site = "https://practicesoftwaretesting.com"; // Toolshop: a public demo shop built for test automation
const string Api = "https://api.practicesoftwaretesting.com";
const string Email = "customer@practicesoftwaretesting.com", Password = "welcome01"; // the site's published demo account

// Fetches every page of the API from inside the page with the token: one by one, then all at once.
const string FetchAll = """
async ({ api, token }) => {
  const get = (n) => fetch(`${api}/invoices?page=${n}`, { headers: { authorization: `Bearer ${token}` } }).then(async (r) => ({ status: r.status, text: await r.text() }));
  const meta = JSON.parse((await get(1)).text);
  const numbers = Array.from({ length: meta.last_page }, (_, i) => i + 1);
  let t = performance.now();
  const pages = [];
  for (const n of numbers) pages.push(await get(n));
  const sequential = (performance.now() - t) / 1000;
  t = performance.now();
  await Promise.all(numbers.map(get));
  const parallel = (performance.now() - t) / 1000;
  const noToken = await fetch(`${api}/invoices?page=1`).then((r) => r.status);
  const items = pages.flatMap((p) => JSON.parse(p.text).data);
  return JSON.stringify({
    sequential, parallel, total: meta.total, perPage: meta.per_page, lastPage: meta.last_page,
    requests: pages.length, bytes: pages.reduce((s, p) => s + p.text.length, 0), statuses: [...new Set(pages.map((p) => p.status))],
    invoices: items.map((i) => i.invoice_number), fields: Object.keys(items[0]).length, noToken,
  });
}
""";

using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);

// 503 means the fleet is momentarily busy: wait Retry-After and try again.
async Task<JsonElement> Launch(JsonObject body)
{
    for (var attempt = 1; ; attempt++)
    {
        var res = await http.PostAsync("https://starter.cdpfleet.com/chromium/session",
            new StringContent(body.ToJsonString(), Encoding.UTF8, "application/json"));
        if (res.IsSuccessStatusCode) return await res.Content.ReadFromJsonAsync<JsonElement>();
        if ((int)res.StatusCode != 503 || attempt == 5) throw new Exception($"launch {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
        await Task.Delay((int)(res.Headers.RetryAfter?.Delta?.TotalMilliseconds ?? 5000));
    }
}

var session = await Launch(new JsonObject { ["proxy"] = Environment.GetEnvironmentVariable("PROXY_URL"), ["headless"] = "new", ["record"] = false });
using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(session.GetProperty("wsUrl").GetString()!, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });
try
{
    var page = await browser.NewPageAsync();
    // The app can take 30 s to render through a rotating residential exit: wait up to 90 s.
    page.SetDefaultTimeout(90000);

    // 1. Log in like a person; the site stores its token and sends it with every API call.
    await page.GotoAsync($"{Site}/auth/login");
    await page.FillAsync("#email", Email);
    await page.FillAsync("#password", Password);
    await page.ClickAsync("[type=submit]");
    await page.WaitForURLAsync("**/account");

    // 2. Find the token twice: in localStorage, and in the header of the page's own API request.
    var stored = await page.EvaluateAsync<string>("() => localStorage.getItem('auth-token')");
    var uiCalls = 0;
    void CountCall(object? _, IRequest r) { if (r.Url.StartsWith($"{Api}/invoices")) uiCalls++; }
    page.Request += CountCall;
    var firstCall = await page.RunAndWaitForRequestAsync(() => page.GotoAsync($"{Site}/account/invoices"), r => r.Url.StartsWith($"{Api}/invoices"));
    var header = (await firstCall.AllHeadersAsync()).GetValueOrDefault("authorization", "");
    var payload = stored.Split('.')[1].Replace('-', '+').Replace('_', '/');
    payload = payload.PadRight(payload.Length + (4 - payload.Length % 4) % 4, '=');
    var claims = JsonDocument.Parse(Convert.FromBase64String(payload)).RootElement;

    // 3. The page's way: click through the pager, reading the table each time.
    await page.WaitForSelectorAsync("table tbody tr");
    var clock = Stopwatch.StartNew();
    async Task<string[]> ReadTable() => await page.EvalOnSelectorAllAsync<string[]>("table tbody tr td:first-child", "tds => tds.map((td) => td.textContent.trim())");
    var viaUi = new List<string>(await ReadTable());
    var lastPage = int.Parse((await page.Locator("button[aria-label^=\"Page-\"]").Last.InnerTextAsync()).Trim());
    for (var n = 2; n <= lastPage; n++)
    {
        var want = $"{Api}/invoices?page={n}";
        await page.RunAndWaitForResponseAsync(() => page.ClickAsync("[data-test=\"pagination-next\"]"), r => r.Url == want);
        await page.WaitForFunctionAsync("first => document.querySelector('table tbody tr td')?.textContent.trim() !== first", viaUi[^15]);
        viaUi.AddRange(await ReadTable());
    }
    var uiSeconds = clock.Elapsed.TotalSeconds;
    page.Request -= CountCall;
    var columns = await page.Locator("table thead th").CountAsync();

    // 4. The API's way: the same token on fetch() from inside the page — the browser makes the
    //    requests, through the session's proxy.
    var viaApi = JsonDocument.Parse(await page.EvaluateAsync<string>(FetchAll, new { api = Api, token = stored })).RootElement;
    var apiInvoices = viaApi.GetProperty("invoices").EnumerateArray().Select((e) => e.GetString()!).ToList();

    var output = new JsonObject
    {
        ["token"] = new JsonObject
        {
            ["in_local_storage"] = stored != null ? "auth-token" : null,
            ["header_matches"] = header == $"Bearer {stored}",
            ["lifetime_seconds"] = claims.GetProperty("exp").GetInt64() - claims.GetProperty("iat").GetInt64(),
            ["seconds_left_at_end"] = claims.GetProperty("exp").GetInt64() - DateTimeOffset.UtcNow.ToUnixTimeSeconds(),
        },
        ["api"] = new JsonObject
        {
            ["total"] = viaApi.GetProperty("total").GetInt32(), ["per_page"] = viaApi.GetProperty("perPage").GetInt32(),
            ["last_page"] = viaApi.GetProperty("lastPage").GetInt32(), ["without_token"] = viaApi.GetProperty("noToken").GetInt32(),
        },
        ["ui"] = new JsonObject { ["pages"] = lastPage, ["invoices"] = viaUi.Count, ["invoice_calls"] = uiCalls, ["columns"] = columns, ["seconds"] = uiSeconds },
        ["direct"] = new JsonObject
        {
            ["requests"] = viaApi.GetProperty("requests").GetInt32(),
            ["statuses"] = JsonNode.Parse(viaApi.GetProperty("statuses").GetRawText()),
            ["invoices"] = apiInvoices.Count,
            ["fields_per_invoice"] = viaApi.GetProperty("fields").GetInt32(),
            ["json_bytes"] = viaApi.GetProperty("bytes").GetInt64(),
            ["seconds_one_by_one"] = viaApi.GetProperty("sequential").GetDouble(),
            ["seconds_all_at_once"] = viaApi.GetProperty("parallel").GetDouble(),
        },
        ["same_invoices"] = viaUi.Count == apiInvoices.Count && viaUi.Order().SequenceEqual(apiInvoices.Order()),
        ["ui_missing_from_api"] = viaUi.Distinct().Except(apiInvoices).Count(),
    };
    Console.WriteLine(output.ToJsonString());
}
finally
{
    await browser.CloseAsync();
}
