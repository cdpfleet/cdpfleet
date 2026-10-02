// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.function.Consumer;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PAGE = "https://www.bbc.com/news";
  static final List<String> FIRST_PARTY = List.of("bbc.com", "bbc.co.uk", "bbci.co.uk"); // the site's own domains and CDNs
  static final Set<String> HEAVY = Set.of("image", "media", "font");
  static final double PRICE_PER_GB = 3; // a typical residential proxy price, USD

  // Load the page in a fresh context (empty cache) and count every byte on the wire.
  static JsonObject measure(Browser browser, String label, String block) {
    BrowserContext context = browser.newContext();
    if (block != null) {
      context.route("**/*", route -> {
        Request r = route.request();
        String host = URI.create(r.url()).getHost();
        boolean thirdParty = FIRST_PARTY.stream().noneMatch(host::endsWith);
        if (HEAVY.contains(r.resourceType()) || (block.equals("first-party") && thirdParty)) route.abort();
        else route.resume();
      });
    }
    Page page = context.newPage();
    long[] bytes = {0};
    int[] counts = {0, 0}; // requests, blocked
    Consumer<Request> count = req -> {
      Sizes s = req.sizes();
      bytes[0] += s.requestHeadersSize + s.requestBodySize + s.responseHeadersSize + s.responseBodySize;
      counts[0]++;
    };
    page.onRequestFinished(count);
    page.onRequestFailed(req -> counts[1]++);
    long t = System.currentTimeMillis();
    // DOM ready, then a fixed 5 s for the rest to arrive: the same window for every variant
    // ('load' can wait forever on a blocked video).
    page.navigate(PAGE, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(90000));
    long readyMs = System.currentTimeMillis() - t;
    page.waitForTimeout(5000);
    String title = page.title();
    page.offRequestFinished(count); // stop counting before closing
    context.close();
    JsonObject row = new JsonObject();
    row.addProperty("variant", label);
    row.addProperty("title", title);
    row.addProperty("requests", counts[0]);
    row.addProperty("blocked", counts[1]);
    row.addProperty("kilobytes", Math.round(bytes[0] / 1024.0));
    row.addProperty("dom_ready_ms", readyMs);
    return row;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": true}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        List<JsonObject> rows = List.of(
            measure(browser, "everything", null),
            measure(browser, "no images, media or fonts", "heavy"),
            measure(browser, "…and first-party only", "first-party"));
        double full = rows.get(0).get("kilobytes").getAsDouble();
        JsonArray out = new JsonArray();
        for (JsonObject r : rows) {
          double kb = r.get("kilobytes").getAsDouble();
          r.addProperty("saved", Math.round((1 - kb / full) * 100) + "%");
          r.addProperty("proxy_cost_per_100k_pages", "$" + Math.round(kb * 100000 / 1024 / 1024 * PRICE_PER_GB));
          out.add(r);
        }
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
