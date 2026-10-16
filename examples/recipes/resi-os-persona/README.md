# cdpfleet residential from Windows devices, with a matching persona

`-os-windows` in a `cdpfleet-resi` token asks for a residential IP that belongs to a Windows device; Camoufox's `os: "windows"` makes the browser report Windows too, and `geoip` takes the timezone and locale from the exit. Measured: a US exit, `navigator.platform` Win32, the exit's timezone in the browser — the IP and the browser tell the same story. Other values: `android`, `ios`, `mac` (availability varies by country). Tier 1 ($5/GB) from your balance; `-tier-2` to `-tier-4` are cheaper tiers. See [residential proxy](https://cdpfleet.com/docs/proxies#residential).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
