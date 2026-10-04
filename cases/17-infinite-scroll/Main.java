// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.*;
import java.nio.charset.StandardCharsets;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String START = "https://quotes.toscrape.com/scroll"; // loads 10 quotes per screen, 100 in all
  static final String QUOTES_JS = "els => els.map(e => ({ text: e.querySelector('.text').textContent, author: e.querySelector('.author').textContent }))";
  static final Gson GSON = new Gson();

  // Way 1: behave like a user — scroll to the bottom until nothing more appears, then read the DOM.
  static JsonObject byScrolling(Page page) {
    long t = System.currentTimeMillis();
    int[] requests = {0};
    page.onRequest(req -> requests[0]++);
    page.navigate(START, new Page.NavigateOptions().setTimeout(60000));
    page.locator(".quote").first().waitFor(new Locator.WaitForOptions().setTimeout(60000));
    long loaded = System.currentTimeMillis();
    int count = 0, scrolls = 0, stale = 0;
    while (stale < 3) {
      page.evaluate("window.scrollTo(0, document.body.scrollHeight)");
      scrolls++;
      page.waitForTimeout(600);
      int now = page.locator(".quote").count();
      stale = now > count ? 0 : stale + 1;
      count = now;
    }
    JsonArray quotes = GSON.toJsonTree(page.evalOnSelectorAll(".quote", QUOTES_JS)).getAsJsonArray();
    long done = System.currentTimeMillis();
    Set<String> authors = new HashSet<>();
    for (JsonElement q : quotes) authors.add(q.getAsJsonObject().get("author").getAsString());
    JsonObject row = new JsonObject();
    row.addProperty("method", "scroll the page");
    row.addProperty("quotes", quotes.size());
    row.addProperty("authors", authors.size());
    row.addProperty("scrolls", scrolls);
    row.addProperty("requests", requests[0]);
    row.addProperty("load_seconds", (loaded - t) / 1000.0);
    row.addProperty("collect_seconds", (done - loaded) / 1000.0);
    return row;
  }

  // The endpoint's URL with its page parameter replaced.
  static String withPage(URI endpoint, int n) {
    Map<String, String> params = new LinkedHashMap<>();
    if (endpoint.getQuery() != null) for (String kv : endpoint.getQuery().split("&")) {
      String[] p = kv.split("=", 2);
      params.put(p[0], p.length > 1 ? p[1] : "");
    }
    params.put("page", String.valueOf(n));
    StringJoiner q = new StringJoiner("&");
    params.forEach((k, v) -> q.add(URLEncoder.encode(k, StandardCharsets.UTF_8) + "=" + URLEncoder.encode(v, StandardCharsets.UTF_8)));
    return endpoint.getScheme() + "://" + endpoint.getAuthority() + endpoint.getPath() + "?" + q;
  }

  // Way 2: catch the JSON request the page makes for its first screen, then call that endpoint
  // yourself from inside the browser (same proxy, cookies and TLS fingerprint as the page),
  // several pages at a time. No scrolling, no guessing when loading has finished.
  static JsonObject byApi(Page page) {
    long t = System.currentTimeMillis();
    int[] requests = {0};
    page.onRequest(req -> requests[0]++);
    Response response = page.waitForResponse(r -> r.url().contains("/api/") && r.request().resourceType().equals("xhr"),
        new Page.WaitForResponseOptions().setTimeout(60000),
        () -> page.navigate(START, new Page.NavigateOptions().setTimeout(60000)));
    long loaded = System.currentTimeMillis();
    URI endpoint = URI.create(response.url());
    JsonArray quotes = new JsonArray();
    quotes.addAll(JsonParser.parseString(response.text()).getAsJsonObject().getAsJsonArray("quotes")); // page 1 came for free
    int wave = 4; // one round trip through the proxy per wave, not per page
    int next = 2;
    for (boolean more = true; more; next += wave) {
      List<String> urls = new ArrayList<>();
      for (int i = 0; i < wave; i++) urls.add(withPage(endpoint, next + i));
      JsonArray pages = GSON.toJsonTree(page.evaluate("us => Promise.all(us.map(u => fetch(u).then(r => r.json())))", urls)).getAsJsonArray();
      more = true;
      for (JsonElement p : pages) {
        quotes.addAll(p.getAsJsonObject().getAsJsonArray("quotes"));
        more &= p.getAsJsonObject().get("has_next").getAsBoolean();
      }
    }
    long done = System.currentTimeMillis();
    Set<String> authors = new HashSet<>();
    for (JsonElement q : quotes) authors.add(q.getAsJsonObject().getAsJsonObject("author").get("name").getAsString());
    JsonObject row = new JsonObject();
    row.addProperty("method", "call its JSON API");
    row.addProperty("quotes", quotes.size());
    row.addProperty("authors", authors.size());
    row.addProperty("api_pages", next - 1);
    row.addProperty("endpoint", endpoint.getPath() + "?page=N");
    row.addProperty("requests", requests[0]);
    row.addProperty("load_seconds", (loaded - t) / 1000.0);
    row.addProperty("collect_seconds", (done - loaded) / 1000.0);
    return row;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray out = new JsonArray();
        Page page = browser.newPage(); // a fresh tab per method, so request counts don't mix
        out.add(byScrolling(page));
        page.close();
        page = browser.newPage();
        out.add(byApi(page));
        page.close();
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
