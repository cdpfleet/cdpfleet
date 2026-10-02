// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.*;
import java.util.function.Consumer;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");

  // What the server saw: header names in order, and the HTTP/2 fingerprint.
  static JsonObject observe(Browser browser, String label, Browser.NewContextOptions options, Consumer<Page> setup) {
    BrowserContext context = browser.newContext(options);
    Page page = context.newPage();
    if (setup != null) setup.accept(page);
    JsonObject fp = JsonParser.parseString(page.navigate("https://tls.peet.ws/api/all",
        new Page.NavigateOptions().setTimeout(60000)).text()).getAsJsonObject();
    context.close();
    JsonArray order = new JsonArray();
    String acceptLanguage = null;
    for (JsonElement f : fp.getAsJsonObject("http2").getAsJsonArray("sent_frames")) {
      if (!f.getAsJsonObject().get("frame_type").getAsString().equals("HEADERS")) continue;
      for (JsonElement h : f.getAsJsonObject().getAsJsonArray("headers")) {
        String line = h.getAsString();
        order.add(line.substring(0, line.indexOf(':', 1)));
        if (line.startsWith("accept-language: ")) acceptLanguage = line.substring(17);
      }
    }
    JsonObject row = new JsonObject();
    row.addProperty("variant", label);
    row.add("header_order", order);
    row.addProperty("accept_language", acceptLanguage);
    row.add("akamai_h2", fp.getAsJsonObject("http2").get("akamai_fingerprint"));
    row.add("ja4", fp.getAsJsonObject("tls").get("ja4"));
    return row;
  }

  public static void main(String[] args) throws Exception {
    String body = "{\"proxy\": " + new Gson().toJson(System.getenv("PROXY_URL")) + ", \"headless\": false}";
    HttpResponse<String> res = HttpClient.newHttpClient().send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/chrome/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(body)).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch: " + res.statusCode() + " " + res.body());
    String wsUrl = JsonParser.parseString(res.body()).getAsJsonObject().get("wsUrl").getAsString();

    try (Playwright playwright = Playwright.create()) {
      Browser browser = playwright.chromium().connect(wsUrl, new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
      try {
        JsonArray rows = new JsonArray();
        rows.add(observe(browser, "default", new Browser.NewContextOptions(), null));
        rows.add(observe(browser, "locale: de-DE", new Browser.NewContextOptions().setLocale("de-DE"), null));
        rows.add(observe(browser, "extraHTTPHeaders", new Browser.NewContextOptions()
            .setExtraHTTPHeaders(Map.of("accept-language", "de-DE", "x-request-id", "abc123")), null));
        rows.add(observe(browser, "route: rewrite headers", new Browser.NewContextOptions(), page -> page.route("**/*", route -> {
          Map<String, String> headers = new HashMap<>(route.request().headers());
          headers.put("x-request-id", "abc123");
          route.resume(new Route.ResumeOptions().setHeaders(headers));
        })));
        System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
      } finally {
        browser.close();
      }
    }
  }
}
