// dotnet add package Microsoft.Playwright --version 1.60.0
// env: CDPFLEET_API_KEY, PROXY_URL_US
using System.Diagnostics;
using System.Net.Http.Json;
using System.Text.Encodings.Web;
using System.Text.Json;
using System.Text.Json.Nodes;
using System.Text.RegularExpressions;
using Microsoft.Playwright;

const string Site = "https://www.saucedemo.com"; // a public demo shop for testing practice
const string Password = "secret_sauce"; // printed on the shop's login page
// The shop's demo users: a normal one, one whose pages are slow, and one with broken inputs.
string[] users = { "standard_user", "performance_glitch_user", "problem_user" };
var buyer = new[] { ("firstName", "Ada"), ("lastName", "Lovelace"), ("postalCode", "10115") };
string[] items = { "sauce-labs-backpack", "sauce-labs-bike-light" };
string T(string sel) => $"[data-test=\"{sel}\"]";
double Money(string s) => double.Parse(Regex.Replace(s, "[^0-9.]", ""), System.Globalization.CultureInfo.InvariantCulture);

var key = Environment.GetEnvironmentVariable("CDPFLEET_API_KEY")!;
using var http = new HttpClient();
http.DefaultRequestHeaders.Add("x-api-key", key);
var res = await http.PostAsJsonAsync("https://starter.cdpfleet.com/chromium/session",
    new { proxy = Environment.GetEnvironmentVariable("PROXY_URL_US"), headless = true });
if (!res.IsSuccessStatusCode) throw new Exception($"launch: {(int)res.StatusCode} {await res.Content.ReadAsStringAsync()}");
var wsUrl = (await res.Content.ReadFromJsonAsync<JsonElement>()).GetProperty("wsUrl").GetString()!;

using var playwright = await Playwright.CreateAsync();
var browser = await playwright.Chromium.ConnectAsync(wsUrl, new() { Headers = new Dictionary<string, string> { ["x-api-key"] = key } });

// The checkout as one user, in a fresh context: every step waits for the page it leads to
// (WaitForURLAsync), never for a fixed time.
async Task<JsonObject> Checkout(string user)
{
    var context = await browser.NewContextAsync();
    var page = await context.NewPageAsync();
    var steps = new JsonObject();
    var sw = Stopwatch.StartNew();
    void Step(string name) { steps[name] = sw.ElapsedMilliseconds; sw.Restart(); }
    var output = new JsonObject
    {
        ["user"] = user, ["empty_form_error"] = null, ["typed_back"] = null, ["step_one_error"] = null, ["item_sum"] = null,
        ["subtotal"] = null, ["tax"] = null, ["total"] = null, ["totals_add_up"] = null, ["completed"] = null,
    };
    try
    {
        await page.GotoAsync(Site);
        await page.FillAsync(T("username"), user);
        await page.FillAsync(T("password"), Password);
        await page.ClickAsync(T("login-button"));
        await page.WaitForURLAsync("**/inventory.html");
        Step("login");

        foreach (var item in items) await page.ClickAsync(T($"add-to-cart-{item}"));
        await page.Locator(T("shopping-cart-badge")).Filter(new() { HasText = items.Length.ToString() }).WaitForAsync();
        await page.ClickAsync(T("shopping-cart-link"));
        await page.WaitForURLAsync("**/cart.html");
        await page.ClickAsync(T("checkout"));
        await page.WaitForURLAsync("**/checkout-step-one.html");
        Step("cart");

        // Step one: an empty form is refused; then type the buyer and read the fields back.
        await page.ClickAsync(T("continue"));
        output["empty_form_error"] = await page.Locator(T("error")).InnerTextAsync();
        foreach (var (field, value) in buyer) await page.FillAsync(T(field), value);
        var typed = new JsonObject();
        foreach (var (field, _) in buyer) typed[field] = await page.InputValueAsync(T(field));
        output["typed_back"] = typed;
        await page.ClickAsync(T("continue"));
        // Either step two loads or the form shows an error: wait for whichever comes first.
        await page.Locator($"{T("subtotal-label")}, {T("error")}").First.WaitForAsync();
        if (!page.Url.Contains("checkout-step-two"))
        {
            output["step_one_error"] = await page.Locator(T("error")).InnerTextAsync();
            Step("step_one");
            output["steps_ms"] = steps;
            return output;
        }
        Step("step_one");

        // Step two: the overview's totals against the item prices.
        var prices = await page.Locator(T("inventory-item-price")).AllInnerTextsAsync();
        var itemSum = Math.Round(prices.Sum(Money), 2);
        var subtotal = Money(await page.Locator(T("subtotal-label")).InnerTextAsync());
        var tax = Money(await page.Locator(T("tax-label")).InnerTextAsync());
        var total = Money(await page.Locator(T("total-label")).InnerTextAsync());
        output["item_sum"] = itemSum;
        output["subtotal"] = subtotal;
        output["tax"] = tax;
        output["total"] = total;
        output["totals_add_up"] = subtotal == itemSum && Math.Abs(subtotal + tax - total) < 0.005;
        await page.ClickAsync(T("finish"));
        await page.WaitForURLAsync("**/checkout-complete.html");
        output["completed"] = await page.Locator(T("complete-header")).InnerTextAsync();
        Step("step_two_and_finish");
        output["steps_ms"] = steps;
        return output;
    }
    finally
    {
        await context.CloseAsync();
    }
}

try
{
    var runs = new JsonArray();
    foreach (var user in users) runs.Add(await Checkout(user));
    Console.WriteLine(runs.ToJsonString(new JsonSerializerOptions { WriteIndented = true, Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping }));
}
finally
{
    await browser.CloseAsync();
}
