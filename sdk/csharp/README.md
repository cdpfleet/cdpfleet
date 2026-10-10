# Cdpfleet

.NET SDK for [cdpfleet](https://cdpfleet.com): launch a remote browser (Chrome, Chromium,
Firefox, Camoufox, WebKit) behind your proxy and connect your own Playwright to it.
The SDK does not bundle Playwright — you pass your `IBrowserType`, so you keep control of
the client version.

```sh
dotnet add package Cdpfleet
dotnet add package Microsoft.Playwright --version 1.60.0
```

```csharp
using Cdpfleet;
using Microsoft.Playwright;

using var playwright = await Playwright.CreateAsync();
var fleet = new CdpfleetClient(); // CDPFLEET_API_KEY from the environment

await using var session = await fleet.LaunchAsync("chromium", new
{
    proxy = Environment.GetEnvironmentVariable("PROXY_URL"),
    headless = true,
});
var browser = await session.ConnectAsync(playwright.Chromium);
var page = await browser.NewPageAsync();
await page.GotoAsync("https://example.com");
Console.WriteLine(await page.TitleAsync());
```

Launch options (anonymous object or `IDictionary<string, object?>`) go to the API unchanged.
`LaunchAsync` retries fleet backpressure (503) and rate limits (429) for up to
`maxWaitSeconds` (120); other failures throw `CdpfleetException` with `Status`, `Code`, `Body`.

## keep_alive

A session launched with `keep_alive = true` survives a disconnect: call `ConnectAsync` again
to re-attach to the same browser, with its pages intact. Disposing the session (or
`CloseAsync`) closes the connected browsers and then ends a keep_alive session; call
`EndAsync()` to end it explicitly.

Also: `LiveLinkAsync("view" | "control")`, `SwapProxyAsync(proxy, proxyRules)`,
`ConnectOverCDPAsync` (launch with `cdp = true`) and the account API (`MeAsync`, `UsageAsync`,
`ThreadsAsync`, `SessionsAsync`, `RecordingsAsync`, `RecordingUrlAsync`, `ProxyBalanceAsync`, …).

Docs: https://cdpfleet.com/docs/sdk
