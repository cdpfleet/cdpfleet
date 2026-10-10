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
  static final String[] URLS = { "https://news.ycombinator.com/", "https://en.wikipedia.org/wiki/Web_scraping", "https://books.toscrape.com/" };
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

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
        JsonArray pages = new JsonArray();
        for (String url : URLS) {
          page.navigate(url, new Page.NavigateOptions().setWaitUntil(WaitUntilState.DOMCONTENTLOADED).setTimeout(60000));
          String html = page.content();
          String text = page.locator("body").innerText();
          String aria = page.locator("body").ariaSnapshot();
          String[] lines = aria.split("\n", -1);

          JsonObject p = new JsonObject();
          p.addProperty("url", url);
          p.addProperty("html_chars", html.length());
          p.addProperty("text_chars", text.length());
          p.addProperty("aria_chars", aria.length());
          p.addProperty("aria_links", aria.split("- link ", -1).length - 1);
          p.addProperty("aria_vs_html", Math.round((double) aria.length() / html.length() * 1000) / 10.0);
          p.addProperty("aria_sample", String.join("\n", Arrays.copyOfRange(lines, 0, Math.min(6, lines.length))));
          pages.add(p);
        }

        JsonObject out = new JsonObject();
        out.add("pages", pages);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    }
  }
}
