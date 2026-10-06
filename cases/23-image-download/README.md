# Download every image on a page without downloading it twice

**Easy** · 2026-10-06 · Chromium · [read it on cdpfleet.com](https://cdpfleet.com/docs/cases/image-download)

> Two ways to get a page's images as bytes on your machine: keep the responses the page already loaded (zero extra requests), or fetch them again from inside the page. Both through the browser, both verified as real JPEGs.

## The problem

Saving the images from a page sounds like a one-liner until you think about where the bytes should come from. Download the URLs with your own HTTP client and you hit the CDN from another IP with another fingerprint and no cookies — hotlink protection and signed URLs are built for exactly that. Fetch them again in the browser and you pay for every image twice through your proxy. But the page already downloaded them: can you just keep those bytes?

## What we used, and why

| What | Why |
|---|---|
| `chromium`, `headless: "new"` | One session, one thread. |
| `page.on("response")` with `request().resourceType() === "image"` and `response.body()` | Keeps the bytes of every image the page loads, as it loads them — no second request. |
| `waitUntil: "networkidle"` | So all cover images have arrived before we count them. |
| `$$eval("article.product_pod img", …)` reading `currentSrc` | The list of images we actually want, matched against what was captured. |
| `fetch(url)` → `arrayBuffer()` → base64 inside `page.evaluate` | The re-fetch alternative: same cookies, proxy and headers as the page, bytes handed to the client as text. |
| The JPEG magic bytes `FF D8 FF` | Proof the bytes are the image, not an error page. |

## How it works

1. Open the page with a response listener that stores the body of every successful image response.
2. List the 20 cover images on the page and match them against the captured responses.
3. Fetch the same 20 images again from inside the page and decode them on the client.
4. For each method: count images, validate JPEG headers, sum the bytes, count extra requests, time it.

## The code

The same program in five languages, each verified on the production fleet (last run 2026-10-06):

- [Node.js](node.mjs) — npm install playwright@1.60.0 && node node.mjs
- [Python](main.py) — pip install playwright==1.60.0 requests aiohttp && python main.py
- [Java](Main.java) — Maven with com.microsoft.playwright:playwright:1.60.0 and com.google.code.gson:gson:2.11.0 (see templates/java), main class Main
- [C#](Program.cs) — dotnet new console, dotnet add package Microsoft.Playwright --version 1.60.0, replace Program.cs, dotnet run
- [Go](main.go) — go mod init example && go get github.com/playwright-community/playwright-go@v0.6000.0 && go run . (driver setup: templates/go/README.md)

Environment: `CDPFLEET_API_KEY`, `PROXY_URL` (see [cases/README.md](../README.md#environment)).

## What we got

| Method | Images | Valid JPEG | Total (KB) | Extra requests | Seconds |
|---|---|---|---|---|---|
| capture responses while the page loads | 20 | 20 | 173 | 0 | 22.159 |
| fetch() each image again in the page | 20 | 20 | 173 | 20 | 10.317 |

IP addresses are replaced with placeholders (203.0.113.x); equal addresses stay equal. Full output: [output.json](output.json).

## Takeaways

- **Capturing responses gives you all 20 images with zero extra requests** — the bytes were already on their way to the renderer; `response.body()` streams them to your client over the fleet connection.
- **Both methods produce the identical 173 KB of valid JPEGs**, so re-fetching buys nothing except a second trip through the proxy for every file.
- **Capture while loading, fetch for the rest:** capture is free for everything the page displays; use the in-page fetch for images the page only links to (full-size originals behind thumbnails) — still as the browser, with its cookies and proxy.
- **The timing difference here is page load, not method:** the capture number includes waiting for `networkidle` through the proxy; the re-fetch ran on a warm page. Count requests and bytes, which are the real cost.
- **Watch for cached responses:** `response.body()` can fail for responses served from cache; the capture code ignores those, which is why the list is matched against the page's images.
