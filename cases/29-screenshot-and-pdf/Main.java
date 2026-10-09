// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import com.microsoft.playwright.options.*;
import java.net.URI;
import java.net.http.*;
import java.nio.file.*;
import java.util.*;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final String PAGE = "https://books.toscrape.com/";
  static final Gson GSON = new GsonBuilder().serializeNulls().create();

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + GSON.toJson(System.getenv("PROXY_URL")) + ", \"headless\": \"new\"}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chromium/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    Path dir = Files.createTempDirectory("cdpfleet-captures");
    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        Page page = browser.newPage(new Browser.NewPageOptions().setViewportSize(1280, 720));
        page.navigate(PAGE, new Page.NavigateOptions().setTimeout(60000).setWaitUntil(WaitUntilState.NETWORKIDLE));

        JsonArray results = new JsonArray();

        long t1 = System.currentTimeMillis();
        Path viewport = dir.resolve("viewport.png");
        page.screenshot(new Page.ScreenshotOptions().setPath(viewport));
        results.add(row("viewport screenshot", Files.size(viewport), System.currentTimeMillis() - t1));

        long t2 = System.currentTimeMillis();
        Path fullPage = dir.resolve("full-page.png");
        page.screenshot(new Page.ScreenshotOptions().setPath(fullPage).setFullPage(true));
        results.add(row("full-page screenshot", Files.size(fullPage), System.currentTimeMillis() - t2));

        long t3 = System.currentTimeMillis();
        Path element = dir.resolve("element.png");
        page.locator(".product_pod").first().screenshot(new Locator.ScreenshotOptions().setPath(element));
        results.add(row("element screenshot", Files.size(element), System.currentTimeMillis() - t3));

        long t4 = System.currentTimeMillis();
        Path pdf = dir.resolve("page.pdf");
        page.pdf(new Page.PdfOptions().setPath(pdf));
        results.add(row("pdf", Files.size(pdf), System.currentTimeMillis() - t4));

        JsonObject out = new JsonObject();
        out.add("captures", results);
        System.out.println(new GsonBuilder().serializeNulls().setPrettyPrinting().disableHtmlEscaping().create().toJson(out));
      } finally {
        browser.close();
      }
    } finally {
      for (Path f : (Iterable<Path>) Files.list(dir)::iterator) Files.deleteIfExists(f);
      Files.deleteIfExists(dir);
    }
  }

  static JsonObject row(String type, long sizeBytes, long ms) {
    JsonObject o = new JsonObject();
    o.addProperty("type", type);
    o.addProperty("file_size_bytes", sizeBytes);
    o.addProperty("seconds", Math.round(ms / 10.0) / 100.0);
    return o;
  }
}
