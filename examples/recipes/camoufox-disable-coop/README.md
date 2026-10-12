# Camoufox that can click inside cross-origin iframes

`disable_coop: true` switches off Cross-Origin-Opener-Policy so your code can click elements inside cross-origin iframes — some CAPTCHA and consent widgets live there. It is detectable, so turn it on only for the sites that need it. Reaching into frames: [iframes and shadow DOM](https://cdpfleet.com/docs/cases/iframes-and-shadow-dom).

- [Node.js](node/example.mjs)
- [Python](python/example.py)
- [Java](java/Example.java)
- [C#](csharp/Program.cs)
- [Go](go/main.go)

Set `CDPFLEET_API_KEY` and replace the placeholder proxy with yours. Install the client named at the top of each file.
