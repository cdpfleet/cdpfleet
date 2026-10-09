// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String START = "https://books.toscrape.com/";
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  static final String EXTRACT_JS = """
      () => [...document.querySelectorAll('article.product_pod')].map(el => {
        const stars = { One: 1, Two: 2, Three: 3, Four: 4, Five: 5 };
        const ratingClass = [...el.querySelector('.star-rating').classList].find(c => c !== 'star-rating');
        return {
          title: el.querySelector('h3 a').getAttribute('title'),
          price: parseFloat(el.querySelector('.price_color').textContent.replace(/[^0-9.]/g, '')),
          rating: stars[ratingClass] || 0,
          in_stock: el.querySelector('.availability').textContent.trim().toLowerCase().includes('in stock'),
        };
      })""";

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
        long t = System.currentTimeMillis();
        JsonArray books = new JsonArray();

        page.navigate(START, new Page.NavigateOptions().setTimeout(60000));
        for (JsonElement e : GSON.toJsonTree(page.evaluate(EXTRACT_JS)).getAsJsonArray()) books.add(e);

        Locator next = page.locator("li.next a");
        if (next.count() > 0) {
          next.click();
          page.waitForLoadState(com.microsoft.playwright.options.LoadState.DOMCONTENTLOADED);
          for (JsonElement e : GSON.toJsonTree(page.evaluate(EXTRACT_JS)).getAsJsonArray()) books.add(e);
        }

        JsonObject out = new JsonObject();
        out.addProperty("pages_scraped", 2);
        out.addProperty("total_books", books.size());
        out.add("books", books);
        out.addProperty("seconds", Math.round((System.currentTimeMillis() - t) / 10.0) / 100.0);

        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
