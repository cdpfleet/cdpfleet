// Maven: com.microsoft.playwright:playwright:1.60.0, com.google.code.gson:gson:2.11.0
// Run with PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1. env: CDPFLEET_API_KEY, PROXY_URL
import com.google.gson.*;
import com.microsoft.playwright.*;
import java.net.URI;
import java.net.http.*;
import java.util.Map;

public class Main {
  static final String KEY = System.getenv("CDPFLEET_API_KEY");
  static final HttpClient HTTP = HttpClient.newHttpClient();

  static JsonObject launch(String name, JsonObject options) throws Exception {
    HttpResponse<String> res = HTTP.send(HttpRequest.newBuilder(URI.create("https://starter.cdpfleet.com/" + name + "/session"))
        .header("x-api-key", KEY).header("content-type", "application/json")
        .POST(HttpRequest.BodyPublishers.ofString(options.toString())).build(), HttpResponse.BodyHandlers.ofString());
    if (res.statusCode() != 200) throw new RuntimeException("launch " + name + ": " + res.statusCode() + " " + res.body());
    return JsonParser.parseString(res.body()).getAsJsonObject();
  }

  // A residential exit occasionally times out: one retry, in a fresh tab.
  static JsonObject fingerprint(Browser browser) {
    for (int attempt = 1; ; attempt++) {
      try {
        Response res = browser.newPage().navigate("https://tls.peet.ws/api/all", new Page.NavigateOptions().setTimeout(30000));
        return JsonParser.parseString(res.text()).getAsJsonObject();
      } catch (PlaywrightException err) {
        if (attempt == 2) throw err;
      }
    }
  }

  public static void main(String[] args) throws Exception {
    // One browser per engine family, and the Playwright client that speaks to it.
    String[][] browsers = {{"chrome", "chromium"}, {"edge", "chromium"}, {"firefox", "firefox"}, {"camoufox", "firefox"}, {"webkit", "webkit"}};
    JsonArray rows = new JsonArray();
    try (Playwright playwright = Playwright.create()) {
      for (String[] b : browsers) {
        JsonObject options = new JsonObject();
        options.addProperty("proxy", System.getenv("PROXY_URL"));
        options.addProperty("headless", true);
        JsonObject session = launch(b[0], options);
        BrowserType family = switch (b[1]) { case "firefox" -> playwright.firefox(); case "webkit" -> playwright.webkit(); default -> playwright.chromium(); };
        Browser browser = family.connect(session.get("wsUrl").getAsString(), new BrowserType.ConnectOptions().setHeaders(Map.of("x-api-key", KEY)));
        try {
          // Navigate the browser itself: the JSON describes the TLS ClientHello and HTTP/2
          // frames this very browser sent (APIRequest would use Playwright's own client).
          JsonObject fp = fingerprint(browser);
          JsonObject tls = fp.getAsJsonObject("tls");
          JsonObject h2 = fp.has("http2") && fp.get("http2").isJsonObject() ? fp.getAsJsonObject("http2") : new JsonObject();
          JsonObject row = new JsonObject();
          row.addProperty("browser", b[0]);
          row.addProperty("version", browser.version());
          row.add("user_agent", fp.get("user_agent"));
          row.add("http_version", fp.get("http_version"));
          row.add("ja4", tls.get("ja4"));
          row.add("ja3_hash", tls.get("ja3_hash"));
          row.add("peetprint_hash", tls.get("peetprint_hash"));
          row.add("akamai_h2", h2.has("akamai_fingerprint") ? h2.get("akamai_fingerprint") : JsonNull.INSTANCE);
          row.add("akamai_h2_hash", h2.has("akamai_fingerprint_hash") ? h2.get("akamai_fingerprint_hash") : JsonNull.INSTANCE);
          row.addProperty("cipher_suites", tls.getAsJsonArray("ciphers").size());
          row.addProperty("extensions", tls.getAsJsonArray("extensions").size());
          rows.add(row);
        } finally {
          browser.close(); // ends the session and stops billing
        }
      }
    }
    System.out.println(new GsonBuilder().setPrettyPrinting().disableHtmlEscaping().create().toJson(rows));
  }
}
