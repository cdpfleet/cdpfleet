// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.WaitUntilState;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.concurrent.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final Gson GSON = new GsonBuilder().serializeNulls().create();
  static final String[] URLS = {
      "https://books.toscrape.com/",
      "https://quotes.toscrape.com/",
      "https://example.com",
      "https://en.wikipedia.org/wiki/Web_scraping",
      "https://news.ycombinator.com/",
  };

  record Result(String url, String title, String method) {}

  static Result extract(Browser browser, String url) {
    Page page = browser.newPage();
    try {
      page.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(60000));
      return new Result(url, page.title(), null);
    } finally {
      page.close();
    }
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
        long t1 = System.currentTimeMillis();
        List<Result> sequential = new ArrayList<>();
        for (String url : URLS) {
          Result r = extract(browser, url);
          sequential.add(new Result(r.url, r.title, "sequential"));
        }
        double seqSeconds = Math.round((System.currentTimeMillis() - t1) / 10.0) / 100.0;

        // Playwright for Java isn't thread-safe: start all five navigations, then wait for each.
        long t2 = System.currentTimeMillis();
        List<Page> pages = new ArrayList<>();
        for (String url : URLS) {
          Page page = browser.newPage();
          page.evaluate("u => { location.href = u; }", url);
          pages.add(page);
        }
        List<Result> parallel = new ArrayList<>();
        for (int i = 0; i < URLS.length; i++) {
          Page page = pages.get(i);
          page.waitForURL(u -> !u.startsWith("about:"), new Page.WaitForURLOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(60000));
          parallel.add(new Result(URLS[i], page.title(), "parallel"));
          page.close();
        }
        double parSeconds = Math.round((System.currentTimeMillis() - t2) / 10.0) / 100.0;

        JsonObject out = new JsonObject();
        out.addProperty("sequential_seconds", seqSeconds);
        out.addProperty("parallel_seconds", parSeconds);
        out.addProperty("speedup", Math.round(seqSeconds / parSeconds * 10) / 10.0 + "x");
        JsonArray results = new JsonArray();
        for (Result r : sequential) results.add(GSON.toJsonTree(r));
        for (Result r : parallel) results.add(GSON.toJsonTree(r));
        out.add("results", results);

        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
