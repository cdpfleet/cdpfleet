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
  static final Gson GSON = new GsonBuilder().serializeNulls().create();
  // A static catalogue, a big article and a page that renders its data with a delayed script.
  static final String[][] PAGES = {
      {"https://books.toscrape.com/", "article.product_pod"},
      {"https://en.wikipedia.org/wiki/Web_scraping", "#mw-content-text p"},
      {"https://quotes.toscrape.com/js-delayed/", ".quote"},
  };
  static final String[] STRATEGIES = {"commit", "domcontentloaded", "load", "networkidle", "selector"};
  static final Map<String, WaitUntilState> WAIT_UNTIL = Map.of(
      "commit", WaitUntilState.COMMIT,
      "domcontentloaded", WaitUntilState.DOMCONTENTLOADED,
      "load", WaitUntilState.LOAD,
      "networkidle", WaitUntilState.NETWORKIDLE);

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
        JsonArray rows = new JsonArray();
        for (String[] p : PAGES) {
          String url = p[0], data = p[1];
          for (String strategy : STRATEGIES) {
            // A fresh context each time: no cache, so every strategy waits for the same work.
            BrowserContext context = browser.newContext();
            Page page = context.newPage();
            long t = System.currentTimeMillis();
            if (strategy.equals("selector")) {
              page.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.COMMIT).setTimeout(60000));
              page.locator(data).first().waitFor(new Locator.WaitForOptions().setTimeout(60000));
            } else {
              page.navigate(url, new Page.NavigateOptions().setWaitUntil(WAIT_UNTIL.get(strategy)).setTimeout(60000));
            }
            long ms = System.currentTimeMillis() - t;
            JsonObject row = new JsonObject();
            row.addProperty("url", url);
            row.addProperty("strategy", strategy);
            row.addProperty("ms", ms);
            row.addProperty("items_ready", page.locator(data).count());
            rows.add(row);
            context.close();
          }
        }

        JsonObject out = new JsonObject();
        out.add("rows", rows);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
