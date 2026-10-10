// SDK smoke test on production: launch → connect → page → live link → keep_alive reconnect → close.
// env: CDPFLEET_API_KEY (a dedicated plan for keep_alive), PROXY_URL. Prints one JSON line.
using System.Text.Json;
using Cdpfleet;
using Microsoft.Playwright;

using var playwright = await Playwright.CreateAsync();
var proxy = Environment.GetEnvironmentVariable("PROXY_URL");
var fleet = new CdpfleetClient();

var s1 = await fleet.LaunchAsync("chromium", new { proxy, headless = true, live_view = true });
var b1 = await s1.ConnectAsync(playwright.Chromium);
var p1 = await b1.NewPageAsync();
await p1.GotoAsync("https://httpbin.org/html");
var title = await p1.Locator("h1").TextContentAsync();
var liveProtocol = (await s1.LiveLinkAsync("view")).GetProperty("protocol").GetString();
await s1.CloseAsync();

var s2 = await fleet.LaunchAsync("chromium", new { proxy, headless = true, keep_alive = true, inactivity_timeout = "2m" });
var b2 = await s2.ConnectAsync(playwright.Chromium);
var p2 = await (await b2.NewContextAsync()).NewPageAsync();
await p2.GotoAsync("https://httpbin.org/html");
await b2.CloseAsync();
b2 = await s2.ConnectAsync(playwright.Chromium);
var keepAlivePageKept = b2.Contexts.SelectMany(c => c.Pages).Any(p => p.Url.Contains("httpbin"));
await s2.CloseAsync();
var ended = !(await fleet.ThreadsAsync()).GetRawText().Contains(s2.Id);
var usageOk = (await fleet.UsageAsync()).TryGetProperty("used_thread_seconds", out var used) && used.ValueKind == JsonValueKind.Number;

Console.WriteLine(JsonSerializer.Serialize(new
{
    title,
    live_protocol = liveProtocol,
    keep_alive_page_kept = keepAlivePageKept,
    ended,
    usage_ok = usageOk,
}));
