# Camoufox with your own full fingerprint

`fingerprint` replaces the generated BrowserForge persona with yours; it must carry a Firefox `navigator.userAgent` (a Chrome or Safari one returns 400). Measured on prod with this launch: the user agent, `navigator.platform` (MacIntel), `hardwareConcurrency` (8), the screen and available sizes and the 30-bit colour depth all read back as given. Two things did not come from the object: the language follows `locale` (or, without it, your proxy's country via geoip; `en-US` with `geoip: false`), and `devicePixelRatio` stayed 1. For one or two changes on top of a generated persona, use `config` instead (see camoufox-config).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
