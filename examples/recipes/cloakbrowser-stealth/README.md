# CloakBrowser: fingerprint-spoofed Chromium for protected sites

CloakBrowser is a Chromium build hardened against fingerprinting. Pages always see 1920x1080 for its fingerprint's screen, regardless of the actual display. Headful here (the default) so the virtual display is used; `screen_size` sizes the virtual display to match CloakBrowser's fingerprinted 1920x1080 so screenshots and live view look right.

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
