// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String START = "https://books.toscrape.com/"; // 70-odd same-site links on the front page
  static final int WAVE = 8; // links checked at once
  static final String LINKS_JS = "(as, origin) => [...new Set(as.map(a => a.href))].filter(h => h.startsWith(origin) && !h.includes('#'))";
  static final String FETCH_JS = "urls => Promise.all(urls.map(async url => {"
      + " try { const r = await fetch(url, { cache: 'no-store' }); return { url, status: r.status, redirected: r.redirected }; }"
      + " catch { return { url, status: 0, redirected: false }; } }))";
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  record Result(String url, int status, boolean redirected) {}

  static JsonObject summary(String method, List<Result> results, double seconds, int requestsMade) {
    List<Result> broken = results.stream().filter(r -> r.status == 0 || r.status >= 400).toList();
    JsonObject row = new JsonObject();
    row.addProperty("method", method);
    row.addProperty("links", results.size());
    row.addProperty("ok", results.stream().filter(r -> r.status >= 200 && r.status < 300).count());
    row.addProperty("redirected", results.stream().filter(r -> r.redirected).count());
    row.addProperty("broken", broken.size());
    JsonArray urls = new JsonArray();
    broken.stream().limit(5).forEach(r -> urls.add(r.url));
    row.add("broken_urls", urls);
    row.addProperty("seconds", seconds);
    row.addProperty("seconds_per_link", Math.round(seconds / results.size() * 100) / 100.0);
    row.addProperty("requests_made", requestsMade);
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
        Page page = browser.newPage();
        page.navigate(START, new Page.NavigateOptions().setTimeout(60000));
        // Every unique same-site link on the page, as absolute URLs.
        URI startUri = URI.create(START);
        String origin = startUri.getScheme() + "://" + startUri.getAuthority();
        List<String> links = new ArrayList<>();
        for (JsonElement e : GSON.toJsonTree(page.evalOnSelectorAll("a[href]", LINKS_JS, origin)).getAsJsonArray()) links.add(e.getAsString());

        // Way 1: fetch() inside the page, WAVE links at a time — the browser's proxy, cookies
        // and TLS, no navigation, no rendering, no assets.
        long t1 = System.currentTimeMillis();
        List<Result> fetched = new ArrayList<>();
        for (int i = 0; i < links.size(); i += WAVE) {
          List<String> wave = links.subList(i, Math.min(i + WAVE, links.size()));
          for (JsonElement e : GSON.toJsonTree(page.evaluate(FETCH_JS, wave)).getAsJsonArray()) {
            JsonObject o = e.getAsJsonObject();
            fetched.add(new Result(o.get("url").getAsString(), o.get("status").getAsInt(), o.get("redirected").getAsBoolean()));
          }
        }
        JsonObject inPage = summary("fetch() in the page, " + WAVE + " at a time", fetched, (System.currentTimeMillis() - t1) / 1000.0, links.size());

        // Way 2: navigate to each link, like a user — full page loads with all their assets.
        // Only a sample: this is the slow way, and it is the same work for every link.
        List<String> sample = links.subList(0, Math.min(10, links.size()));
        int[] requests = {0};
        page.onRequest(req -> requests[0]++);
        long t2 = System.currentTimeMillis();
        List<Result> navigated = new ArrayList<>();
        for (String url : sample) {
          try {
            Response r = page.navigate(url, new Page.NavigateOptions().setTimeout(60000));
            navigated.add(new Result(url, r != null ? r.status() : 0, r != null && !r.url().equals(url)));
          } catch (PlaywrightException e) {
            navigated.add(new Result(url, 0, false));
          }
        }
        JsonObject byNav = summary("page.goto each link (10-link sample)", navigated, (System.currentTimeMillis() - t2) / 1000.0, requests[0]);

        JsonArray out = new JsonArray();
        out.add(inPage);
        out.add(byNav);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
