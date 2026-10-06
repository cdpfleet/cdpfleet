// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PAGE = "https://books.toscrape.com/"; // 20 cover images

  // Fetch each image again from inside the page and hand the bytes over as base64.
  static final String REFETCH = """
      urls => Promise.all(urls.map(async (url) => {
        const buf = await (await fetch(url, { cache: 'no-store' })).arrayBuffer();
        let s = ''; const b = new Uint8Array(buf); for (let i = 0; i < b.length; i++) s += String.fromCharCode(b[i]);
        return { url, b64: btoa(s) };
      }))""";

  record Img(String url, byte[] bytes) {}

  static boolean isJpeg(byte[] b) {
    return b.length > 3 && (b[0] & 0xff) == 0xff && (b[1] & 0xff) == 0xd8 && (b[2] & 0xff) == 0xff;
  }

  static JsonObject summary(String method, List<Img> files, double seconds, int extraRequests) {
    long total = 0;
    int valid = 0;
    for (Img f : files) { total += f.bytes().length; if (isJpeg(f.bytes())) valid++; }
    JsonObject o = new JsonObject();
    o.addProperty("method", method);
    o.addProperty("images", files.size());
    o.addProperty("valid_jpeg", valid);
    o.addProperty("total_kb", Math.round(total / 1024.0));
    o.addProperty("extra_requests", extraRequests);
    o.addProperty("seconds", seconds);
    return o;
  }

  public static void main(String[] args) throws Exception {
    HttpClient http = HttpClient.newHttpClient();
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = http.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        // Way 1: keep the bytes the page downloads anyway — zero extra requests. The listener
        // only remembers the response; bodies are read after navigation.
        Page page = browser.newPage();
        List<Response> imageResponses = Collections.synchronizedList(new ArrayList<>());
        page.onResponse(r -> { if ("image".equals(r.request().resourceType()) && r.ok()) imageResponses.add(r); });
        long t1 = System.currentTimeMillis();
        page.navigate(PAGE, new Page.NavigateOptions().setTimeout(60000).setWaitUntil(WaitUntilState.NETWORKIDLE));
        @SuppressWarnings("unchecked")
        List<String> covers = (List<String>) page.evalOnSelectorAll("article.product_pod img", "imgs => imgs.map(i => i.currentSrc || i.src)");
        List<Img> fromLoad = new ArrayList<>();
        for (Response r : new ArrayList<>(imageResponses)) {
          try { if (covers.contains(r.url())) fromLoad.add(new Img(r.url(), r.body())); } catch (PlaywrightException e) { /* body gone (cache) */ }
        }
        JsonObject way1 = summary("capture responses while the page loads", fromLoad, (System.currentTimeMillis() - t1) / 1000.0, 0);

        // Way 2: fetch each image again from inside the page (same cookies, proxy and headers).
        long t2 = System.currentTimeMillis();
        @SuppressWarnings("unchecked")
        List<Map<String, Object>> again = (List<Map<String, Object>>) page.evaluate(REFETCH, covers);
        List<Img> refetched = new ArrayList<>();
        for (Map<String, Object> a : again) refetched.add(new Img((String) a.get("url"), Base64.getDecoder().decode((String) a.get("b64"))));
        JsonObject way2 = summary("fetch() each image again in the page", refetched, (System.currentTimeMillis() - t2) / 1000.0, covers.size());

        JsonArray out = new JsonArray();
        out.add(way1);
        out.add(way2);
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().serializeNulls().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
